import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import { translate } from '../../test-utils/i18n'
import type { CorrectionDocument, CorrectionState } from '../../api/business'
import { requestFence } from '../lifecycle/model'
import * as model from './model'

const source = parse(readFileSync(new URL('./CorrectionWorkspace.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText
const cursors = { page: 1, page_size: 20, total: 0, items: [] }
const settle = () => new Promise<void>(resolve => setImmediate(resolve))

function mountSetup({
  query = {} as Record<string, string>,
  correctionDocuments = async (_kb: string, _filters: { state: CorrectionState }) => cursors,
  correctionDocument = async (_kb: string, _document: string, _version: string, _state: CorrectionState): Promise<CorrectionDocument> => {
    throw new Error('unexpected detail read')
  },
  correctionMarkdown = async (_kb: string, _document: string, _version: string, _state: CorrectionState): Promise<CorrectionDocument> => {
    throw new Error('unexpected Markdown export')
  },
  canWrite = (_kb: string): boolean => false,
} = {}) {
  const scope = vue.effectScope()
  const focused: string[] = []
  const route = vue.reactive({ query })
  const props = vue.reactive({ scope: 'kb-a', knowledgeBases: [{ id: 'kb-a', name: 'A' }, { id: 'kb-b', name: 'B' }] })
  const modules: Record<string, unknown> = {
    vue: { ...vue, onBeforeUnmount: vue.onScopeDispose },
    'vue-i18n': { useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => translate(key, params), locale: { value: 'zh-CN' } }) },
    'vue-router': { useRoute: () => route, useRouter: () => ({}) },
    dompurify: {},
    '@/api/business': { correctionDocuments, correctionDocument, correctionMarkdown },
    '@/api/lifecycle': { authority: async (kb: string) => ({ can_write: canWrite(kb) }) },
    '@/utils/documentPreviewMarkdown': {},
    '@/utils/desktopDownload': { saveBusinessDownload: async () => ({ mode: 'browser' }) },
    '../lifecycle/model': { requestFence },
    './model': model,
  }
  const setup = new Function('require', 'defineProps', 'defineEmits', 'exports', code + '\nreturn { scopeMode, draft, applyFilters, detail, detailError, download, selectedItem, detailHeading, itemHeading };')
  const state = scope.run(() => setup((id: string) => modules[id], () => props, () => () => {}, {}))
  state.detailHeading.value = { focus: () => focused.push('document') }
  state.itemHeading.value = { focus: () => focused.push('item') }
  return { state, route, props, focused, stop: () => scope.stop() }
}

function projectedDocument(state: CorrectionDocument['items'][number]['state'] = 'active'): CorrectionDocument {
  return {
    knowledge_base_id: 'kb-b', document_id: 'doc', version_id: 'v1', document_title: 'Source',
    file_name: 'Source反馈文档.md', active_count: 1, last_approved_at: '2026-11-20T09:00:00Z', markdown: 'Approved correction',
    items: [{ id: 'item', revision: 2, document_id: 'doc', version_id: 'v1', chunk_id: 'chunk', state, correction: 'Approved correction',
      applies_when: '', approved_at: '2026-11-20T09:00:00Z', effective_at: '2026-11-20T09:00:00Z' }],
  }
}

const query = { kb: 'kb-a', document_id: 'doc', version_id: 'v1', correctionKB: 'kb-b', correction: 'item', correctionRevision: '2' }

test('all authorized scopes fan out per KB and history only queries writable KBs', async () => {
  const calls: unknown[] = []
  const view = mountSetup({
    canWrite: kb => kb === 'kb-b',
    correctionDocuments: async (kb, filters) => { calls.push([kb, filters.state]); return cursors },
  })
  try {
    await settle()
    assert.deepEqual(calls.splice(0), [['kb-a', 'active']])
    view.state.scopeMode.value = 'all'
    await settle()
    assert.deepEqual(calls.splice(0), [['kb-a', 'active'], ['kb-b', 'active']])
    view.state.draft.state = 'all'
    view.state.applyFilters()
    await settle()
    assert.deepEqual(calls, [['kb-b', 'all']])
  } finally { view.stop() }
})

test('a citation uses the exact KB and source version, then focuses the exact revision', async () => {
  const calls: unknown[] = []
  const view = mountSetup({ query, correctionDocument: async (...args) => { calls.push(args); return projectedDocument() } })
  try {
    await settle()
    assert.deepEqual(calls, [['kb-b', 'doc', 'v1', 'active']])
    assert.equal(view.state.selectedItem.value?.revision, 2)
    assert.deepEqual(view.focused, ['document', 'item'])
    view.route.query = { ...query, correctionRevision: '1' }
    await settle()
    assert.equal(view.state.selectedItem.value, undefined)
  } finally { view.stop() }
})

test('ordinary detail refuses history, unknown KBs, and an out-of-state projection', async () => {
  const calls: unknown[] = []
  const view = mountSetup({
    query: { ...query, correctionState: 'all' },
    correctionDocument: async (...args) => { calls.push(args); return projectedDocument('withdrawn') },
  })
  try {
    await settle()
    assert.equal(view.state.detail.value, undefined)
    assert.ok(view.state.detailError.value)
    assert.deepEqual(calls, [])
    view.route.query = { ...query, correctionKB: 'unavailable' }
    await settle()
    assert.equal(view.state.detail.value, undefined)
    assert.deepEqual(calls, [])
    view.route.query = query
    await settle()
    assert.deepEqual(calls, [['kb-b', 'doc', 'v1', 'active']])
    assert.equal(view.state.detail.value, undefined)
    assert.ok(view.state.detailError.value)
  } finally { view.stop() }
})

test('a late historic response cannot render after the KB leaves the authorized catalog', async () => {
  let release: ((value: CorrectionDocument) => void) | undefined
  const view = mountSetup({ query: { ...query, correctionState: 'withdrawn' }, canWrite: () => true,
    correctionDocument: async () => new Promise(resolve => { release = resolve }) })
  try {
    await settle()
    assert.ok(release)
    view.props.knowledgeBases = [{ id: 'kb-a', name: 'A' }]
    release(projectedDocument('withdrawn'))
    await settle()
    assert.equal(view.state.detail.value, undefined)
    assert.ok(view.state.detailError.value)
  } finally { view.stop() }
})

test('archive download reauthorizes using the exact library, version and state', async () => {
  const calls: unknown[] = []
  const view = mountSetup({
    query: { ...query, correctionState: 'withdrawn' }, canWrite: () => true,
    correctionDocument: async () => projectedDocument('withdrawn'),
    correctionMarkdown: async (...args) => { calls.push(args); throw new Error('read_only') },
  })
  try {
    await settle()
    assert.ok(view.state.detail.value)
    await view.state.download()
    assert.deepEqual(calls, [['kb-b', 'doc', 'v1', 'withdrawn']])
    assert.equal(view.state.detail.value, undefined)
    assert.ok(view.state.detailError.value)
  } finally { view.stop() }
})

test('correction documents use the native workbench list skeleton', () => {
  const file = readFileSync(new URL('./CorrectionWorkspace.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="correction-list-panel workspace-list-panel"'), 'the list must be a transparent native panel')
  assert.ok(template.includes('class="workspace-list-head"'), 'the list needs a sticky column header')
  assert.ok(template.includes('class="workspace-list-row correction-row"'), 'correction documents render as grid rows')
  assert.ok(!template.includes('<t-table'), 'the old fixed-width TDesign table must be gone')
  assert.ok(file.includes('.correction-list-panel'), 'the scoped shell must expose the native list panel')
})
