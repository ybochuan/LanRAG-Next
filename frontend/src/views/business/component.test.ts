import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import { translate } from '../../test-utils/i18n'
import * as model from './model'
import { requestFence } from '../lifecycle/model'

// Execute the actual setup script with controlled API completion and router hooks.
function mountSetup(
  api: Record<string, unknown>,
  lifecycle: Record<string, unknown> = {},
  props: { section: 'associations' | 'feedback-governance' | 'revisions'; scope?: string; canWrite?: boolean; inboxOnly?: boolean } = { section: 'revisions' },
  auth = vue.reactive({ effectiveTenantId: 7, isLoggedIn: true, admin: false }),
  routeQuery: Record<string, unknown> = {},
  native: Record<string, unknown> = {},
) {
  const guards: (() => boolean | void)[] = []
  const events: unknown[][] = []
  const scope = vue.effectScope()
  let overviewCalls = 0
  let nextID = 0
  class Element {
    dataset: Record<string, string> = {}
    focuses = 0
    focus() { this.focuses++ }
  }
  const source = parse(readFileSync(new URL('./BusinessView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const modules: Record<string, unknown> = {
    vue: { ...vue, useId: () => 'test-' + nextID++, onMounted() {}, onBeforeUnmount() {} },
    'vue-router': { useRoute: () => ({ query: routeQuery }), useRouter: () => ({ options: { history: { state: {} } } }), onBeforeRouteLeave: (guard: () => boolean) => guards.push(guard), onBeforeRouteUpdate: (guard: () => boolean) => guards.push(guard) },
    '@/stores/auth': { useAuthStore: () => ({ get effectiveTenantId() { return auth.effectiveTenantId }, get isLoggedIn() { return auth.isLoggedIn }, hasRole: () => auth.admin }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => translate(key, params), locale: { value: 'zh-CN' } }) },
    '@/api/knowledge-base': { listKnowledgeBases: async () => ({ success: true, data: [] }), ...native },
    '@/api/lifecycle': {
      authority: async () => { overviewCalls++; return { can_write: true } },
      ...lifecycle,
    },
    '@/api/business': { documentTitle: async () => '', relationRecords: api.list, ...api },
    './CreateForm.vue': {}, './DocumentPicker.vue': {}, '../lifecycle/model': { requestFence }, './model': model,
  }
  const state = scope.run(() => new Function('require', 'defineProps', 'defineEmits', 'defineExpose', 'setInterval', 'clearInterval', 'window', 'exports', `${code}\nreturn {kb,detail,pending,canWrite,perform,beginWrite,confirmWrite,resetScope,loadTitles,loadRecords,loadAuthority,loadKBs,titles,records,error,authorityError,feedbackTab,queryDocument,queryResult,queryRelated,correctionSource,correctionSourceLoading,correctionSourceError,loadCorrectionSource, recordReaderEntries, readRecord, readerState, restoreReaderState};`)(
    (id: string) => modules[id], () => props, () => (...args: unknown[]) => events.push(args), () => {}, () => 0, () => {}, { addEventListener() {}, removeEventListener() {} }, {},
  ))
  return { state, events, guards, props, overviewCalls: () => overviewCalls, stop: () => scope.stop() }
}

test('association reader uses the authorized record endpoints and their fixed versions', async () => {
  const view = mountSetup({ list: async () => [] }, {}, { section: 'associations', scope: 'kb-a' })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.detail.value = { id: 'relation-a', revision: 3, knowledge_base_id: 'kb-c', document_id: 'source',
      source_version_id: 'v1', target_knowledge_base_id: 'kb-b', target_document_id: 'target', target_version_id: 'v3' }
    view.state.readRecord(view.state.recordReaderEntries.value[0])
    assert.deepEqual(view.events.at(-1), ['read', {
      left: { kb: 'kb-c', id: 'source', version: 'v1' }, right: { kb: 'kb-b', id: 'target', version: 'v3' },
      source: { kind: 'business', section: 'associations', record: 'relation-a', evidence: 'relation-a' },
    }])
    view.state.pending.value = { intent: { action: 'relation_confirm' } }
    view.state.readRecord(view.state.recordReaderEntries.value[0])
    assert.equal(view.events.filter(event => event[0] === 'read').length, 1)
  } finally { view.stop() }
})

test('feedback with restricted evidence never offers a reader entry', async () => {
  const view = mountSetup({ list: async () => [] }, {}, { section: 'feedback-governance', scope: 'kb-a' })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.detail.value = { id: 'feedback-a', revision: 3, feedback_context: { snapshot_restricted: true }, citation_document_ids: ['protected'] }
    assert.deepEqual(view.state.recordReaderEntries.value, [])
  } finally { view.stop() }
})

const correctionQuery = { kb: 'kb-a', guidance: 'correction-a', correctionDocument: 'doc-a', correctionVersion: 'v1', correctionRevision: '2' }

for (const type of ['manual', 'file'] as const) {
  test('a correction selects the existing ' + type + ' revision flow for the exact source', async () => {
    const calls: unknown[][] = []
    const view = mountSetup({ list: async () => [], verifyCorrectionRevision: async (...args: unknown[]) => {
      calls.push(args)
      return '制度 A'
    } }, {}, { section: 'revisions', scope: 'kb-a', canWrite: true }, undefined, correctionQuery, {
      getKnowledgeDetails: async (id: string) => ({ success: true, data: { id, type, knowledge_base_id: 'kb-a', metadata: { status: 'publish' } } }),
    })
    try {
      view.state.kb.value = 'kb-a'; await vue.nextTick()
      view.state.canWrite.value = true
      await view.state.loadCorrectionSource()
      const selection = { id: 'correction-a', document_id: 'doc-a', version_id: 'v1', revision: 2 }
      assert.deepEqual(calls.at(-1), ['kb-a', selection])
      assert.deepEqual(view.state.correctionSource.value, { ...selection, title: '制度 A', type })
    } finally { view.stop() }
  })
}

test('a correction route fails closed on a foreign source, draft manual, or lost write authority', async () => {
  let sourceRead: () => Promise<unknown> = async () => ({ success: true, data: { id: 'doc-a', type: 'file', knowledge_base_id: 'kb-b' } })
  const view = mountSetup({ list: async () => [], verifyCorrectionRevision: async () => '制度 A' }, {},
    { section: 'revisions', scope: 'kb-a', canWrite: true }, undefined, correctionQuery, {
      getKnowledgeDetails: () => sourceRead(),
    })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.canWrite.value = true
    await view.state.loadCorrectionSource()
    assert.equal(view.state.correctionSource.value, undefined)
    assert.match(view.state.correctionSourceError.value, /无法核对/)
    sourceRead = async () => ({ success: true, data: { id: 'doc-a', type: 'manual', knowledge_base_id: 'kb-a', metadata: { status: 'draft' } } })
    await view.state.loadCorrectionSource()
    assert.equal(view.state.correctionSource.value, undefined)
    assert.match(view.state.correctionSourceError.value, /不支持正式修订/)
    let release!: (value: unknown) => void
    sourceRead = () => new Promise(resolve => { release = resolve })
    const check = view.state.loadCorrectionSource()
    await Promise.resolve()
    view.state.canWrite.value = false
    release({ success: true, data: { id: 'doc-a', type: 'file', knowledge_base_id: 'kb-a' } })
    await check
    assert.equal(view.state.correctionSource.value, undefined)
    assert.equal(view.state.correctionSourceLoading.value, false)
  } finally { view.stop() }
})

test('unknown write keeps its original retry when navigating to another business view', async () => {
  const view = mountSetup({ list: async () => [] })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    const pending = { kb: 'kb-a', intent: model.buildIntent('feedback_create', undefined, { topic: 'topic', text: 'content' }, 'original-operation') }
    view.state.pending.value = pending
    assert.equal(view.guards.length, 2)
    for (const guard of view.guards) assert.equal(guard(), false)
    assert.equal(view.state.pending.value.intent.body.operation_id, 'original-operation')
    view.state.pending.value = undefined
    for (const guard of view.guards) assert.equal(guard(), true)
  } finally { view.stop() }
})

test('document titles are resolved once in a single batch with unique ids', async () => {
  const calls: Array<{ kb: string; ids: string[] }> = []
  const view = mountSetup({
    documentTitles: async (kb: string, ids: string[]) => {
      calls.push({ kb, ids })
      return { 'doc-a': '文档 A', 'doc-b': '文档 B' }
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    await view.state.loadTitles(['doc-a', 'doc-b', 'doc-a'])
    assert.deepEqual(calls, [{ kb: 'kb-a', ids: ['doc-a', 'doc-b'] }])
    assert.equal(view.state.titles.value.get('doc-a'), '文档 A')
    assert.equal(view.state.titles.value.get('doc-b'), '文档 B')
  } finally { view.stop() }
})

test('current relations resolve titles for the selected document and peers', async () => {
  const view = mountSetup({
    list: async () => [],
    related: async () => [{ document_id: 'peer', edge_type: 'cites', direction: 'incoming', confidence: 1 }],
    documentTitles: async () => ({ source: '制度 A', peer: '引用 B' }),
  }, {}, { section: 'associations' })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.queryDocument.value = 'source'
    await view.state.queryRelated()
    assert.equal(view.state.queryResult.value[0].document_id, 'peer')
    assert.equal(view.state.titles.value.get('source'), '制度 A')
    assert.equal(view.state.titles.value.get('peer'), '引用 B')
  } finally { view.stop() }
})

test('changing the selected document discards an earlier relation response', async () => {
  let release!: (rows: unknown[]) => void
  const view = mountSetup({
    list: async () => [],
    related: async () => new Promise(resolve => { release = resolve }),
  }, {}, { section: 'associations' })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.queryDocument.value = 'old'
    const old = view.state.queryRelated()
    view.state.queryDocument.value = 'new'
    release([{ document_id: 'old-peer', edge_type: 'cites', direction: 'incoming', confidence: 1 }])
    await old
    assert.equal(view.state.queryResult.value, undefined)
  } finally { view.stop() }
})

test('authority failure is separated from the record-list error', async () => {
  const view = mountSetup(
    { list: async () => [] },
    { authority: async () => { throw { $httpStatus: 503 } } },
  )
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    await view.state.loadAuthority()
    assert.equal(view.state.error.value, '')
    assert.match(view.state.authorityError.value, /权限信息暂不可用/)
  } finally { view.stop() }
})

test('embedded business view reuses the authority supplied by LifecycleView without loading overview', async () => {
  const view = mountSetup({ list: async () => [] }, {}, { section: 'revisions', scope: 'kb-a', canWrite: true })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    await view.state.loadAuthority()
    assert.equal(view.state.canWrite.value, true)
    assert.equal(view.overviewCalls(), 0)
  } finally { view.stop() }
})

test('completed write cannot replace new scope details after its slow list refresh', async () => {
  let release!: (rows: unknown[]) => void
  let defer = false
  const view = mountSetup({
    write: async () => ({ id: 'old-proposal', revision: 2, status: 'approved' }),
    list: async (kb: string, _kind: string, _page: number, id?: string) => {
      if (defer && kb === 'kb-a' && !id) return new Promise(resolve => { release = resolve })
      return []
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    view.state.canWrite.value = true
    view.state.pending.value = { kb: 'kb-a', intent: model.buildIntent('revision_approve', { id: 'old-proposal', revision: 1, status: 'pending_review' }, {}, 'approve') }
    defer = true
    const operation = view.state.perform(); await vue.nextTick()
    assert.equal(typeof release, 'function')
    view.state.kb.value = 'kb-b'; await vue.nextTick()
    view.state.detail.value = { id: 'new-proposal', revision: 4, status: 'pending_review' }
    release([]); await operation
    assert.equal(view.state.detail.value?.id, 'new-proposal')
  } finally { view.stop() }
})

test('publishing a split part submits its revision and reloads the batch', async () => {
  const batch = { id: 'batch', status: 'approved', revision: 2, record_kind: 'split_batch' }
  const part = { id: 'part', status: 'indexed', revision: 4, record_kind: 'split_part', split_id: batch.id }
  const refreshed = { ...batch, split_parts: [{ ...part, status: 'published', revision: 5 }] }
  const writes: unknown[] = []
  const view = mountSetup({
    list: async (_kb: string, _kind: string, _page: number, id?: string) => !id || id === batch.id ? [refreshed] : [],
    write: async (_kb: string, action: string, body: unknown) => {
      writes.push([action, body])
      return { ...part, status: 'published', revision: 5 }
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    view.state.canWrite.value = true
    view.state.detail.value = batch
    await view.state.beginWrite('revision_publish', {}, part)
    await view.state.confirmWrite()
    assert.equal((writes[0] as [string, { expected_revision: number }])[0], 'revision_publish')
    assert.equal((writes[0] as [string, { expected_revision: number }])[1].expected_revision, 4)
    assert.equal(view.state.detail.value.id, batch.id)
    assert.equal(view.state.detail.value.split_parts[0].status, 'published')
  } finally { view.stop() }
})

test('late title response from the previous KB cannot overwrite current titles', async () => {
  let releaseOld!: (titles: Record<string, string>) => void
  const view = mountSetup({
    list: async () => [],
    documentTitles: async (kb: string) => {
      if (kb === 'kb-a') return new Promise(resolve => { releaseOld = resolve })
      return { 'doc-b': 'B' }
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    const old = view.state.loadTitles(['doc-a'])
    view.state.kb.value = 'kb-b'; await vue.nextTick()
    await view.state.loadTitles(['doc-b'])
    releaseOld({ 'doc-a': 'A' }); await old; await vue.nextTick()
    assert.equal(view.state.titles.value.get('doc-b'), 'B')
    assert.equal(view.state.titles.value.has('doc-a'), false)
  } finally { view.stop() }
})

test('late list response from the previous KB cannot replace current records', async () => {
  let releaseOld!: (rows: unknown[]) => void
  const view = mountSetup({
    list: async (kb: string, _kind: string, _page: number, id?: string) => {
      if (!id && kb === 'kb-a') return new Promise(resolve => { releaseOld = resolve })
      if (!id && kb === 'kb-b') return [{ id: 'new-record', revision: 1, status: 'draft' }]
      return []
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    assert.equal(typeof releaseOld, 'function')
    view.state.kb.value = 'kb-b'; await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.records.value[0]?.id, 'new-record')
    releaseOld([{ id: 'old-record', revision: 1, status: 'draft' }]); await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.records.value[0]?.id, 'new-record')
  } finally { view.stop() }
})

test('late list response from the previous business tab cannot replace current records', async () => {
  let releaseOld!: (rows: unknown[]) => void
  const props: { section: 'feedback-governance' } = { section: 'feedback-governance' }
  const view = mountSetup({
    list: async (_kb: string, kind: string, _page: number, id?: string) => {
      if (!id && kind === 'feedback') return new Promise(resolve => { releaseOld = resolve })
      if (!id && kind === 'guidance') return [{ id: 'guidance-record', revision: 1, status: 'active' }]
      return []
    },
  }, {}, props)
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    assert.equal(typeof releaseOld, 'function')
    view.state.feedbackTab.value = 'guidance'; await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.records.value[0]?.id, 'guidance-record')
    releaseOld([{ id: 'old-feedback', revision: 1, status: 'pending' }]); await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.records.value[0]?.id, 'guidance-record')
  } finally { view.stop() }
})

test('business records use the native workbench list skeleton', () => {
  const file = readFileSync(new URL('./BusinessView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="records-panel workspace-list-panel"'), 'records panel must be a transparent native list panel')
  assert.ok(template.includes('class="workspace-list-head"'), 'records need a sticky column header')
  assert.ok(template.includes('class="workspace-list-row"'), 'records render as grid rows')
  assert.ok(template.includes('workspace-cell workspace-cell-main'), 'records keep a flexible primary cell')
  assert.ok(!template.includes('class="record-card"'), 'the old card-like button list must be gone')
  assert.ok(template.includes('class="workspace-compact-meta"'), 'narrow layouts need the compact metadata fallback')
})
