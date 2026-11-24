import assert from 'node:assert/strict'
import test from 'node:test'
import { effectScope, ref } from 'vue'
import type { RelatedDocument } from '@/api/business'
import { useReaderPickerFiles } from './readerPicker'
import { sameKBRelations, verifyRelationTargets } from './readerRelations'

const source = { kb: 'kb-a', id: 'source' }
const doc = (id: string, kb = source.kb, type = 'file') => ({ id, knowledge_base_id: kb, type, title: 'Document ' + id })
const page = (index: number, total: number, data: ReturnType<typeof doc>[], pageSize = 50) => ({
  success: true, page: index, page_size: pageSize, total, data,
})
const row = (id: string, fields: Partial<RelatedDocument> = {}): RelatedDocument => ({
  document_id: id, knowledge_base_id: source.kb, direction: 'outgoing', edge_type: 'cites', confidence: 1, ...fields,
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

test('picker uses native search and metadata to page past filtered results and deduplicate options', async () => {
  const scope = effectScope()
  try {
    const calls: unknown[][] = []
    const picker = scope.run(() => useReaderPickerFiles(ref('kb-a'), ref('  source.pdf  '), () => 'scope-a', async (kb, params) => {
      calls.push([kb, params.page, params.page_size, params.keyword])
      if (params.page === 1) return page(1, 5, [doc('foreign', 'kb-b'), doc('text', 'kb-a', 'url')], 2)
      if (params.page === 2) return page(2, 5, [doc('matching-filename'), doc('manual', 'kb-a', 'manual')], 2)
      return page(3, 5, [doc('matching-filename')], 2)
    }))!
    await Promise.resolve()
    assert.equal(picker.filesEmpty.value, false)
    assert.equal(picker.hasMoreFiles.value, true)
    assert.equal(picker.fileOptions.value.length, 0)
    await picker.loadMoreFiles()
    picker.selection.value = 'manual'
    await picker.loadMoreFiles()
    assert.deepEqual(calls, [['kb-a', 1, 50, 'source.pdf'], ['kb-a', 2, 2, 'source.pdf'], ['kb-a', 3, 2, 'source.pdf']])
    assert.deepEqual(picker.fileOptions.value.map(option => option.value), ['matching-filename', 'manual'])
    assert.equal(picker.selection.value, 'manual')
    assert.equal(picker.hasMoreFiles.value, false)
  } finally {
    scope.stop()
  }
})

test('picker paginates a 51-document result without truncating the first page', async () => {
  const scope = effectScope()
  try {
    const calls: number[] = []
    const items = Array.from({ length: 51 }, (_, index) => doc('doc-' + index))
    const picker = scope.run(() => useReaderPickerFiles(ref('kb-a'), ref(''), () => 'scope-a', async (_kb, params) => {
      calls.push(params.page)
      return params.page === 1 ? page(1, 51, items.slice(0, 50)) : page(2, 51, items.slice(50))
    }))!
    await Promise.resolve()
    assert.equal(picker.fileOptions.value.length, 50)
    assert.equal(picker.hasMoreFiles.value, true)
    await picker.loadMoreFiles()
    assert.equal(picker.fileOptions.value.length, 51)
    assert.deepEqual(calls, [1, 2])
  } finally {
    scope.stop()
  }
})

test('picker ignores in-flight pages after scope switches away and back', async () => {
  const scope = effectScope()
  try {
    const account = ref('first')
    const requests: ReturnType<typeof deferred<ReturnType<typeof page>>>[] = []
    const picker = scope.run(() => useReaderPickerFiles(ref('kb-a'), ref(''), () => account.value, () => {
      const next = deferred<ReturnType<typeof page>>()
      requests.push(next)
      return next.promise
    }))!
    requests[0].resolve(page(1, 51, [doc('old')]))
    await Promise.resolve()
    picker.selection.value = 'old'
    const oldLoad = picker.loadMoreFiles()
    account.value = 'second'
    account.value = 'first'
    assert.equal(picker.selection.value, '')
    assert.equal(picker.fileOptions.value.length, 0)
    requests[3].resolve(page(1, 1, [doc('current')]))
    await Promise.resolve()
    requests[1].resolve(page(2, 51, [doc('stale-load-more')]))
    requests[2].resolve(page(1, 1, [doc('other-scope')]))
    await oldLoad
    assert.deepEqual(picker.fileOptions.value.map(option => option.value), ['current'])
    assert.equal(picker.hasMoreFiles.value, false)
  } finally {
    scope.stop()
  }
})

test('picker rejects mismatched page responses and can retry the same page', async () => {
  const scope = effectScope()
  try {
    let attempts = 0
    const picker = scope.run(() => useReaderPickerFiles(ref('kb-a'), ref(''), () => 'scope-a', async () => {
      attempts++
      return attempts === 1 ? page(2, 1, [doc('incorrect')]) : page(1, 0, [])
    }))!
    await Promise.resolve()
    assert.equal(picker.filesError.value, true)
    assert.equal(picker.filesEmpty.value, false)
    assert.equal(picker.fileOptions.value.length, 0)
    await picker.loadMoreFiles()
    assert.equal(picker.filesError.value, false)
    assert.equal(picker.filesEmpty.value, true)
  } finally {
    scope.stop()
  }
})

test('same-KB relations use explicit endpoint identity before native target lookup', async () => {
  const confirmed = row('allowed', { relation_id: 'rel-a' })
  const incoming = row('allowed', { direction: 'incoming', relation_id: 'rel-b' })
  const candidates = sameKBRelations(source, [confirmed, { ...confirmed }, incoming, row('unreadable'),
    row('other-authorized-kb', { knowledge_base_id: 'kb-b' }), row('legacy', { knowledge_base_id: undefined }), row(source.id)])
  const result = await verifyRelationTargets(source.kb, candidates, async (kb, ids) => {
    assert.equal(kb, source.kb)
    assert.deepEqual(ids, ['allowed', 'unreadable'])
    return new Map([['allowed', 'Verified title'], ['other-authorized-kb', 'Foreign title']])
  }, new AbortController().signal)
  assert.equal(result.status, 'ready')
  assert.deepEqual(result.items.map(item => item.row), [confirmed, incoming])
  assert.deepEqual(result.items.map(item => item.title), ['Verified title', 'Verified title'])
  assert.notEqual(result.items[0].key, result.items[1].key)
})

test('relation target verification exposes partial results only after operational failures and honors authorization denial', async () => {
  const candidates = Array.from({ length: 51 }, (_, index) => row('doc-' + index))
  const signal = new AbortController().signal
  const unavailable = new Error('unavailable')
  const nativeLookup = async (_: string, ids: string[]) => {
    if (ids.length === 1) throw unavailable
    return new Map([['doc-0', 'Still verified']])
  }
  const partial = await verifyRelationTargets(source.kb, candidates, nativeLookup, signal)
  assert.equal(partial.status, 'partial')
  assert.deepEqual(partial.items.map(item => item.row.document_id), ['doc-0'])
  await assert.rejects(verifyRelationTargets(source.kb, candidates.slice(-1), nativeLookup, signal), unavailable)

  const forbidden = { $httpStatus: 403 }
  await assert.rejects(verifyRelationTargets(source.kb, candidates, async (_, ids) => {
    if (ids.length === 1) throw forbidden
    return new Map([['doc-0', 'Must disappear']])
  }, signal), error => error === forbidden)
})

test('abandoned verification does not request remaining targets', async () => {
  const controller = new AbortController()
  const candidates = Array.from({ length: 51 }, (_, index) => row('doc-' + index))
  let batches = 0
  const request = verifyRelationTargets(source.kb, candidates, async () => {
    batches++
    controller.abort()
    return new Map([['doc-0', 'Stale title']])
  }, controller.signal)
  await assert.rejects(request, { name: 'AbortError' })
  assert.equal(batches, 1)
})
