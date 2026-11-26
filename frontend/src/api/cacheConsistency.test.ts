import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { fixture, flush } from '../test-utils/vueFixture.ts'
import * as requestCache from './requestCache.ts'

const { cacheKey, cached, clearRequestCache, invalidateCache, peekCache } = requestCache

test('cached shares one in-flight request and cleans it after a failure', async () => {
  clearRequestCache()
  let loads = 0
  let release!: (value: string) => void
  const loader = async () => {
    loads += 1
    return new Promise<string>((resolve) => { release = resolve })
  }

  const first = cached('doc-titles|kb|["a"]', loader, 60_000)
  const second = cached('doc-titles|kb|["a"]', loader, 60_000)
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(loads, 1)

  release('ok')
  assert.deepEqual(await Promise.all([first, second]), ['ok', 'ok'])

  clearRequestCache()
  let attempts = 0
  const flaky = async () => {
    attempts += 1
    if (attempts === 1) throw new Error('temporary failure')
    return 'recovered'
  }
  await assert.rejects(cached('doc-titles|kb|["b"]', flaky, 60_000))
  assert.equal(await cached('doc-titles|kb|["b"]', flaky, 60_000), 'recovered')
  assert.equal(attempts, 2)
})

test('invalidation during an in-flight request does not leave stale cache', async () => {
  clearRequestCache()
  let release!: (value: string) => void
  const request = cached('doc-titles|kb|["a"]', async () => new Promise<string>((resolve) => { release = resolve }), 60_000)
  await new Promise((resolve) => setImmediate(resolve))
  invalidateCache('doc-titles')
  release('stale')
  assert.equal(await request, 'stale')
  assert.equal(peekCache('doc-titles|kb|["a"]'), undefined)
})

test('cache TTL and prefix invalidation are independent per key', async () => {
  clearRequestCache()
  let loads = 0
  const loader = async () => ++loads
  assert.equal(await cached('doc-titles|kb-a|["a"]', loader, 5), 1)
  assert.equal(await cached('doc-titles|kb-a|["a"]', loader, 5), 1)
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(await cached('doc-titles|kb-a|["a"]', loader, 5), 2)

  assert.equal(await cached('document-picker-active|tenant-a|kb-a|1', loader, 60_000), 3)
  invalidateCache('doc-titles')
  assert.equal(peekCache('doc-titles|kb-a|["a"]'), undefined)
  assert.equal(peekCache('document-picker-active|tenant-a|kb-a|1'), 3)
})

test('documentTitles normalizes id order and reuses one cache entry', async (t) => {
  clearRequestCache()
  const calls: Array<{ query: string; kb: string }> = []
  const business = fixture(t).evaluate(
    `import { URLSearchParams } from 'node:url'\n${readFileSync(new URL('./business.ts', import.meta.url), 'utf8')}`,
    {
      'node:url': { URLSearchParams },
      '@/utils/request': { get: async () => ({}), put: async () => ({}) },
      '@/api/knowledge-base': {
        batchQueryKnowledge: async (query: string, kb: string) => {
          calls.push({ query, kb })
          return { success: true, data: [
            { id: 'doc-a', title: 'A', knowledge_base_id: 'kb-a' },
            { id: 'doc-b', title: 'B', knowledge_base_id: 'kb-a' },
          ] }
        },
      },
      './requestCache': requestCache,
    },
  )

  const first = await business.documentTitles('kb-a', ['doc-b', 'doc-a', 'doc-b'])
  assert.equal(JSON.stringify(first), JSON.stringify({ 'doc-a': 'A', 'doc-b': 'B' }))
  assert.equal(calls.length, 1)
  assert.equal(calls[0]!.query, 'ids=doc-a&ids=doc-b')
  assert.equal(calls[0]!.kb, 'kb-a')

  const second = await business.documentTitles('kb-a', ['doc-a', 'doc-b'])
  assert.equal(JSON.stringify(second), JSON.stringify(first))
  assert.equal(calls.length, 1)
})

test('KB list cache is tenant-scoped and invalidated by KB mutations', async (t) => {
  const store = new Map<string, string>([['weknora_selected_tenant_id', 'tenant-a']])
  const f = fixture(t, {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => { store.set(key, value) },
    removeItem: (key: string) => { store.delete(key) },
  })
  let listCalls = 0
  let postCalls = 0
  const api = f.evaluate(
    `import { URLSearchParams } from 'node:url'\n${readFileSync(new URL('./knowledge-base/index.ts', import.meta.url), 'utf8')}`,
    {
      'node:url': { URLSearchParams },
      '../../utils/request': {
        get: async () => { listCalls += 1; return { success: true, data: [] } },
        post: async () => { postCalls += 1; return { success: true } },
        put: async () => ({ success: true }),
        del: async () => ({ success: true }),
        postUpload: async () => ({ success: true }),
        getDown: async () => ({}),
      },
      './knowledgeFileListQuery': {},
      './knowledgeReadOptions': {},
      '../requestCache': requestCache,
    },
  )

  api.invalidateKnowledgeBaseListCache()
  await api.listKnowledgeBases()
  await api.listKnowledgeBases()
  assert.equal(listCalls, 1)

  await api.createKnowledgeBase({ name: 'new KB' })
  assert.equal(postCalls, 1)
  await api.listKnowledgeBases()
  assert.equal(listCalls, 2)

  const titleKey = cacheKey('doc-titles', '', 'kb-a', ['doc-a'])
  await cached(titleKey, async () => 'title', 60_000)
  await api.deleteKnowledgeBase('kb-a')
  assert.equal(peekCache(titleKey), undefined)
  await api.listKnowledgeBases()
  assert.equal(listCalls, 3)

  store.set('weknora_selected_tenant_id', 'tenant-b')
  await api.listKnowledgeBases()
  assert.equal(listCalls, 4)
})

test('manual document updates invalidate the batch title cache', async (t) => {
  clearRequestCache()
  const f = fixture(t)
  const api = f.evaluate(
    `import { URLSearchParams } from 'node:url'\n${readFileSync(new URL('./knowledge-base/index.ts', import.meta.url), 'utf8')}`,
    {
      'node:url': { URLSearchParams },
      '../../utils/request': {
        get: async () => ({ success: true, data: [] }),
        post: async () => ({ success: true }),
        put: async () => ({ success: true }),
        del: async () => ({ success: true }),
        postUpload: async () => ({ success: true }),
        getDown: async () => ({}),
      },
      './knowledgeFileListQuery': {},
      './knowledgeReadOptions': {},
      '../requestCache': requestCache,
    },
  )
  const key = cacheKey('doc-titles', '', 'kb-a', ['doc-a'])
  const pickerKey = cacheKey('document-picker-active', '', 'kb-a', 1)
  await cached(key, async () => 'old title', 60_000)
  await cached(pickerKey, async () => 'old documents', 5_000)
  await api.updateManualKnowledge('doc-a', { title: 'new title', content: 'body', status: 'published' })
  assert.equal(await cached(key, async () => 'new title', 60_000), 'new title')
  assert.equal(peekCache(pickerKey), undefined)

  await cached(pickerKey, async () => 'old documents', 5_000)
  await api.delKnowledgeDetails('doc-a')
  assert.equal(peekCache(key), undefined)
  assert.equal(peekCache(pickerKey), undefined)

  await cached(pickerKey, async () => 'old documents', 5_000)
  await api.batchDeleteKnowledge('kb-a', ['doc-a'])
  assert.equal(peekCache(pickerKey), undefined)
})

test('DocumentPicker cache key includes tenant and KB', async (t) => {
  clearRequestCache()
  const store = new Map<string, string>([['weknora_selected_tenant_id', 'tenant-a']])
  const f = fixture(t, {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => { store.set(key, value) },
    removeItem: (key: string) => { store.delete(key) },
  })
  let calls = 0
  const dependencies = {
    '@/api/lifecycle': {
      overview: async () => {
        calls += 1
        return { documents: [], total: 0 }
      },
    },
    '@/api/requestCache': requestCache,
  }
  const picker = () => f.component(new URL('../views/business/DocumentPicker.vue', import.meta.url), dependencies, {
    kb: 'kb-a', modelValue: '', label: 'document',
  })

  const first = picker()
  await flush()
  assert.equal(calls, 1)
  first.close()

  const second = picker()
  await flush()
  assert.equal(calls, 1)
  second.close()

  store.set('weknora_selected_tenant_id', 'tenant-b')
  const third = picker()
  await flush()
  assert.equal(calls, 2)
  third.close()
})

test('DocumentPicker searches active titles from page one without reusing unfiltered cache', async (t) => {
  clearRequestCache()
  const calls: unknown[][] = []
  const picker = fixture(t).component(new URL('../views/business/DocumentPicker.vue', import.meta.url), {
    '@/api/lifecycle': {
      overview: async (...args: unknown[]) => {
        calls.push(args)
        return { documents: [], total: 50 }
      },
    },
    '@/api/requestCache': requestCache,
  }, { kb: 'kb-a', modelValue: '', label: 'document', searchable: true })
  await flush()
  picker.vm.keyword = ' 管理办法 '
  await flush()
  t.mock.timers.tick(350)
  await flush()
  assert.deepEqual(calls.at(-1), ['kb-a', 1, 'active', '管理办法', 50])
})

test('lifecycle saves invalidate the document picker cache', async (t) => {
  clearRequestCache()
  const f = fixture(t)
  const api = f.evaluate(
    readFileSync(new URL('./lifecycle.ts', import.meta.url), 'utf8'),
    {
      '@/utils/request': {
        get: async () => ({ success: true, data: {} }),
        put: async () => ({ success: true, data: {} }),
      },
      './requestCache': requestCache,
    },
  )
  const key = cacheKey('document-picker-active', '', 'kb-a', 1)
  await cached(key, async () => 'old documents', 5_000)
  await api.saveAnnotations('kb-a', 'doc-a', 1, { revision: 1, labels: {} })
  assert.equal(peekCache(key), undefined)
})
