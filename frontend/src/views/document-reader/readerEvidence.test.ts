import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test, { type TestContext } from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as model from './model'
import type * as readerApi from './readerApi'

const currentDoc = { kb: 'library', id: 'parent' }
const metadata = { id: 'parent', knowledge_base_id: 'library', file_name: 'parent.pdf', file_type: 'pdf', type: 'file', file_hash: 'parent-hash' }
const child = { id: 'child', relation_id: 'relation', relation_type: 'attachment_of', state: 'current', revision: 1, file_hash: 'child-hash' }
const workbench = { parent_id: 'parent', parent_version: 'parent-hash', items: [child] }
const attachmentTitles = new Map([['child', 'Child']])
const nextTask = () => new Promise<void>(resolve => setImmediate(resolve))

function compile(source: string) {
  return ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
}

function moduleLoader(modules: Record<string, unknown>) {
  return (id: string) => {
    assert.ok(id in modules, `Unexpected import: ${id}`)
    return modules[id]
  }
}

function loadReaderAPI(native: () => Promise<unknown>, get: () => Promise<unknown>) {
  const code = compile(readFileSync(new URL('./readerApi.ts', import.meta.url), 'utf8'))
  const exports = {}
  new Function('require', 'exports', code)(moduleLoader({
    '@/api/knowledge-base': { getKnowledgeDetails: native },
    '@/api/business': {},
    '@/utils/request': { get },
    '../business/model': { httpStatus: (error: { $httpStatus?: number }) => error?.$httpStatus },
  }), exports)
  return exports as typeof readerApi
}

function runComponent<T>(name: string, exposed: string, props: object, modules: Record<string, unknown>) {
  const source = parse(readFileSync(new URL(`./${name}.vue`, import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = compile(source)
  const scope = vue.effectScope()
  const session = vue.reactive({ scope: 'tenant-A' })
  const events: unknown[][] = []
  const require = moduleLoader({
    vue, './model': model,
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key, locale: vue.ref('en-US') }) },
    '@/stores/documentReader': { useDocumentReaderStore: () => session },
    ...modules,
  })
  const state = scope.run(() => new Function('require', 'exports', 'defineProps', 'defineEmits', `${code}
return { ${exposed} };`)(
    require, {}, () => props, () => (...args: unknown[]) => events.push(args),
  )) as T
  return { scope, session, state, events }
}

function attachments(t: TestContext, doc: model.ReaderDocument, list: () => Promise<unknown>) {
  const api = loadReaderAPI(async () => ({ success: true, data: metadata }), async () => new Blob())
  const result = runComponent<{ select: (item: typeof child, compare: boolean) => Promise<void>; error: vue.Ref<string>; items: vue.Ref<unknown[]> }>(
    'ReaderAttachments', 'select, error, items', { doc }, {
      '@/api/attachmentWorkbench': { attachmentWorkbench: list },
      './readerApi': { ...api, readerTitles: async () => attachmentTitles, readerMetadata: async ({ id }: model.ReaderDocument) =>
        id === 'parent' ? metadata : { id, file_hash: 'child-hash' } },
    },
  )
  t.after(() => result.scope.stop())
  return result
}

test('fixed evidence never asks native metadata or preview for current bytes', async () => {
  const unexpected = async () => assert.fail('fixed evidence attempted a native document request')
  const api = loadReaderAPI(unexpected, unexpected)
  const signal = new AbortController().signal
  await assert.rejects(api.readCurrentFile({ ...currentDoc, version: 'observed-node' }, signal), { code: 'unverified' })
  await assert.rejects(api.readCurrentFile({ ...currentDoc, kind: 'correction' }, signal), { code: 'unverified' })
})

test('ordinary viewing checks metadata before and after fetching the authenticated preview', async () => {
  const calls: string[] = []
  const blob = new Blob(['ordinary'])
  let after = metadata
  const api = loadReaderAPI(async () => {
    calls.push('metadata')
    return { success: true, data: calls.length === 1 ? metadata : after }
  }, async () => { calls.push('preview'); return blob })
  const signal = new AbortController().signal
  assert.equal((await api.readCurrentFile(currentDoc, signal)).blob, blob)
  assert.deepEqual(calls, ['metadata', 'preview', 'metadata'])
  calls.length = 0
  after = { ...metadata, file_hash: 'replacement' }
  await assert.rejects(api.readCurrentFile(currentDoc, signal), { code: 'changed' })
})

test('fixed records never request the current attachment list', async t => {
  let calls = 0
  const { state } = attachments(t, { ...currentDoc, version: 'observed-node' }, async () => { calls++; return workbench })
  await nextTask()
  assert.equal(calls, 0)
  assert.deepEqual(state.items.value, [])
})

test('an attachment document shows its parent attachment family instead of failing closed', async t => {
  const childDoc = { kb: 'library', id: 'child' }
  const childMetadata = { id: 'child', knowledge_base_id: 'library', title: 'Child', file_name: 'child.pdf', file_type: 'pdf', type: 'file', file_hash: 'child-hash' }
  const parentMetadata = { id: 'parent', knowledge_base_id: 'library', title: 'Parent', file_name: 'parent.pdf', file_type: 'pdf', type: 'file', file_hash: 'parent-hash' }
  const sibling = { id: 'sibling', relation_id: 'relation-2', relation_type: 'attachment_of', state: 'current', revision: 1, file_hash: 'sibling-hash' }
  const response = { parent_id: 'parent', parent_version: 'parent-hash', parent_file_name: 'parent.pdf', items: [child, sibling] }
  const api = loadReaderAPI(async () => ({ success: true, data: childMetadata }), async () => new Blob())
  const result = runComponent<{ items: vue.Ref<{ id: string }[]>; error: vue.Ref<string>; parentTitle: vue.Ref<string> }>('ReaderAttachments', 'items, error, parentTitle', { doc: childDoc }, {
    '@/api/attachmentWorkbench': { attachmentWorkbench: async () => response },
    './readerApi': { ...api, readerTitles: async () => new Map([['sibling', 'Sibling']]), readerMetadata: async ({ id }: model.ReaderDocument) => id === 'parent' ? parentMetadata : childMetadata },
  })
  t.after(() => result.scope.stop())
  await nextTask()
  assert.deepEqual(result.state.items.value.map(item => item.id), ['sibling'])
  assert.equal(result.state.error.value, undefined)
  assert.equal(result.state.parentTitle.value, 'Parent')
})

test('an attachment document refuses a parent redirect that omits the current document', async t => {
  const childDoc = { kb: 'library', id: 'child' }
  const childMetadata = { id: 'child', knowledge_base_id: 'library', file_name: 'child.pdf', file_type: 'pdf', type: 'file', file_hash: 'child-hash' }
  const response = { parent_id: 'parent', parent_version: 'parent-hash', items: [{ ...child, id: 'sibling' }] }
  const api = loadReaderAPI(async () => ({ success: true, data: childMetadata }), async () => new Blob())
  const result = runComponent<{ items: vue.Ref<unknown[]>; error: vue.Ref<string> }>('ReaderAttachments', 'items, error', { doc: childDoc }, {
    '@/api/attachmentWorkbench': { attachmentWorkbench: async () => response },
    './readerApi': { ...api, readerTitles: async () => new Map(), readerMetadata: async () => childMetadata },
  })
  t.after(() => result.scope.stop())
  await nextTask()
  assert.equal(result.state.error.value, 'missing')
  assert.deepEqual(result.state.items.value, [])
})

test('attachment actions fail closed when a child digest changes without a revision change', async t => {
  let calls = 0
  const { state, events } = attachments(t, currentDoc, async () => ++calls === 1 ? workbench : {
    ...workbench, items: [{ ...child, file_hash: 'changed-child-hash' }],
  })
  await nextTask()
  assert.equal(state.items.value.length, 1)
  await state.select(child, false)
  assert.equal(state.error.value, 'stale')
  assert.deepEqual(events, [])
  assert.deepEqual(state.items.value, [])
})

test('a pending attachment action cannot emit after scope changes', async t => {
  let calls = 0
  let release!: (response: typeof workbench) => void
  const { state, session, events } = attachments(t, currentDoc, async () => ++calls === 2 ? new Promise(resolve => { release = resolve }) : workbench)
  await nextTask()
  const selection = state.select(child, true)
  session.scope = 'tenant-B'
  release(workbench)
  await selection
  assert.deepEqual(events, [])
})

test('ordinary history resolves the current observation before fetching ancestry', async t => {
  const calls: [string, string][] = []
  const expectRoot = 'current-observation'
  const node = { id: expectRoot, type: 'document_version', document_id: 'parent', knowledge_base_id: 'library', current: true }
  const result = runComponent<{ nodes: vue.Ref<unknown[]>; error: vue.Ref<unknown> }>('ReaderHistory', 'nodes, error', { doc: currentDoc, mode: 'history' }, {
    '@/api/lifecycle': { documentDetail: async () => ({}) },
    '@/api/families': {},
    '@/api/lineage': { loadLineageGraph: async (_kb: string, mode: string, root: string) => {
      calls.push([mode, root])
      return { mode, root_id: root, nodes: [node], edges: [] }
    } },
    '../business/model': {}, '../lineage/model': {},
    './readerApi': { readerMetadata: async () => metadata, readerTitles: async () => new Map([['parent', 'Parent']]) },
  })
  t.after(() => result.scope.stop())
  await nextTask()
  assert.deepEqual(calls, [['relations', 'parent'], ['version-ancestry', expectRoot]])
  assert.equal(result.state.error.value, undefined)
  assert.deepEqual(result.state.nodes.value, [node])
})

test('replacement comparison opens the two business documents instead of version observations', () => {
  const source = readFileSync(new URL('./ReaderHistory.vue', import.meta.url), 'utf8')
  const button = source.match(/<t-button[^>]*@click="emit\('compare'[\s\S]*?<\/t-button>/)?.[0]
  assert.ok(button, 'replacement compare button is rendered')
  assert.match(button, /id: step\.old_document_id/)
  assert.match(button, /id: step\.new_document_id/)
  assert.doesNotMatch(button, /old_version_node_id|new_version_node_id/)
})
