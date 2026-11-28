import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createRenderer, defineComponent, h, nextTick, reactive } from 'vue'
import { resolveReferenceSource, type ReferenceSourceTarget } from '../utils/referenceSources.ts'
import { manualCitationFingerprint } from '../views/document-reader/model'

const require = createRequire(import.meta.url)
const { descriptor } = parse(readFileSync(new URL('./ChatReferenceSourceView.vue', import.meta.url), 'utf8'))
const compiled = ts.transpileModule(compileScript(descriptor, {
  id: 'reference-source-test', inlineTemplate: true,
}).content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const flush = async () => { await new Promise(resolve => setImmediate(resolve)); await nextTick() }
const citedMarkdown = '| Model | Flagged as AI | Judged human |\n| --- | --- | --- |\n| v2 | 11 / 210 (5%) | 95% |\n\nFlagged less than half as often as the previous release.'
const refs = [{ id: 'chunk-1', knowledge_id: 'manual-doc', knowledge_title: 'Model card', knowledge_source: 'manual', chunk_type: 'text', content: citedMarkdown }]
const target = resolveReferenceSource(refs, { chunkId: 'chunk-1', anchorText: '本次版本被标记的次数不到上一版本的一半。', openSource: true })!

async function fixture(options: { details?: Record<string, unknown>; chunk?: Record<string, unknown>; target?: ReferenceSourceTarget; readerQuestion?: string } = {}) {
  const props = reactive({ target: options.target || target, active: true })
  const requests: string[] = [], events: string[] = []
  const navigations: any[] = [], citations: any[][] = []
  let openReader: (() => Promise<void>) | undefined
  let preview: any, mounts = 0
  const exports: any = {}
  const Preview = defineComponent({
    props: { knowledgeId: String, fileType: String, fileName: String, locate: Object, sourceMode: Boolean, active: Boolean, toolbarTarget: Object, fillHeight: Boolean },
    setup(p) { preview = p; mounts++; return () => h('preview') },
  })
  runInNewContext(compiled, { exports, console, require(name: string) {
    if (name === 'vue') return require('vue')
    if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
    if (name === 'vue-router') return { useRouter: () => ({
      resolve: () => ({ href: '/knowledge' }),
      currentRoute: { value: { params: { chatid: 'fixture-chat' } } },
      push: (location: unknown) => navigations.push(location),
    }) }
    if (name === '@/views/document-reader/model') return require('../views/document-reader/model.ts')
    if (name === '@/stores/documentReader') return { useDocumentReaderStore: () => ({
      captureCitation: (...args: any[]) => { citations.push(args); return 'citation-ticket' },
      questionOrigin: (id: string) => id === 'fixture-chat' ? require('../views/document-reader/model.ts').readReaderQuestion(options.readerQuestion) : undefined,
    }) }
    // The KB-name lookup rides the shared keyed cache; in the sandbox it
    // resolves instantly so the provenance line renders deterministically.
    if (name === '@/stores/chatResources') return {
      useChatResourcesStore: () => ({ fetchKnowledgeBaseById: async () => ({ name: '测试知识库' }) }),
    }
    if (name === '@/api/knowledge-base/index') return {
      getKnowledgeDetails: async (id: string) => {
        requests.push(`details:${id}`)
        return { data: { type: 'manual', source: 'manual', file_type: 'manual', file_name: 'Model card.md', file_path: '', ...options.details } }
      },
      getChunkByIdOnly: async (id: string) => {
        requests.push(`chunk:${id}`)
        return { data: { knowledge_id: props.target.knowledgeId, chunk_type: 'text', content_revision: 0, content: id === 'chunk-2' ? 'The second cited paragraph.' : citedMarkdown, source_locators: null, ...options.chunk } }
      },
    }
    if (name === '@/components/document-preview.vue') return Preview
    if (name.endsWith('.vue')) return { render: () => null }
    if (name.startsWith('@/utils/')) return require(`../utils/${name.slice('@/utils/'.length)}.ts`)
    throw new Error(`Unexpected import: ${name}`)
  } })
  type Host = { parent?: Host; children: Host[]; text?: string }
  const node = (): Host => ({ children: [] })
  const renderer = createRenderer<Host, Host>({
    createElement: node, createText: (text: string) => ({ ...node(), text }), createComment: node,
    insert(child, parent) { child.parent = parent; parent.children.push(child) },
    remove(child) { const siblings = child.parent?.children; if (siblings) siblings.splice(siblings.indexOf(child), 1) },
    setElementText(el, text) { el.text = text }, setText(el, text) { el.text = text }, patchProp() {}, parentNode: el => el.parent || null, nextSibling: () => null,
  })
  const app = renderer.createApp({ setup: () => () => h(exports.default, { ...props, onUnavailable: () => events.push('unavailable') }) })
  app.component('t-button', defineComponent({
    setup(_, { slots, attrs }) {
      return () => {
        if (slots.default?.().some(node => node.children === 'reader.readOriginal')) openReader = attrs.onClick as () => Promise<void>
        return null
      }
    },
  }))
  for (const name of ['t-icon', 't-loading']) app.component(name, { render: () => null })
  const root = node()
  app.mount(root)
  await flush()
  return { props, requests, events, navigations, citations, openReader: () => { assert.ok(openReader); return openReader() },
    preview: () => preview, mounts: () => mounts, dispose: () => app.unmount(), root }
}

test('manual citation opens Markdown with raw chunk evidence even without source locators', async t => {
  const f = await fixture(); t.after(f.dispose)
  assert.deepEqual(f.events, [])
  const preview = f.preview()
  assert.ok(preview, 'manual entries must not fall back to the reference list')
  assert.equal(preview.knowledgeId, 'manual-doc', 'use the existing authenticated preview endpoint')
  assert.equal(preview.fileType, 'md', 'manual is a storage type, not a preview extension')
  assert.equal(preview.sourceMode, true)
  assert.equal(preview.locate.sourceMarkdown, citedMarkdown)
  assert.equal(preview.locate.sentence, target.anchorText)
  assert.equal(preview.locate.unavailable, false)
  assert.equal(preview.locate.locators.length, 0)
})

test('another citation in the same manual document updates location without remounting', async t => {
  const f = await fixture(); t.after(f.dispose)
  const firstToken = f.preview().locate.token
  f.props.target = { ...target, chunkId: 'chunk-2', anchorText: 'The second cited paragraph.' }
  await flush()
  assert.equal(f.mounts(), 1)
  assert.equal(f.requests.filter(r => r.startsWith('details:')).length, 1)
  assert.equal(f.preview().locate.sourceMarkdown, 'The second cited paragraph.')
  assert.ok(f.preview().locate.token > firstToken)
})

test('manual documents without a file name still open as Markdown', async t => {
  const f = await fixture({ details: { file_name: '' } }); t.after(f.dispose)
  assert.equal(f.preview().fileName, 'Model card')
  assert.equal(f.preview().fileType, 'md')
})

for (const chunk of [{ content_revision: 1 }, { knowledge_id: 'another-document' }]) {
  test(`manual citations reject stale or mismatched evidence: ${JSON.stringify(chunk)}`, async t => {
    const f = await fixture({ chunk }); t.after(f.dispose)
    assert.equal(f.preview().locate.unavailable, true)
    assert.equal(f.preview().locate.sourceMarkdown, undefined)
    assert.equal(f.preview().locate.quotes.length, 0)
  })
}

test('manual document cards and summaries open without inventing a passage', async t => {
  for (const options of [{ target: { ...target, chunkId: undefined } }, { chunk: { chunk_type: 'summary' } }]) {
    const f = await fixture(options); t.after(f.dispose)
    assert.ok(f.preview())
    assert.equal(f.preview().locate, null)
  }
})

test('FAQ still falls back to the source list even with manual source metadata', async t => {
  const f = await fixture({ details: { type: 'faq' } }); t.after(f.dispose)
  assert.equal(f.preview(), undefined)
  assert.deepEqual(f.events, ['unavailable'])
})

test('uploaded files keep their original preview format', async t => {
  const f = await fixture({ details: { type: 'file', source: 'upload', file_type: 'pdf', file_name: 'Report.pdf' }, target: { ...target, knowledgeSource: 'upload' } }); t.after(f.dispose)
  assert.equal(f.preview().fileType, 'pdf')
})

function nodeText(root: any): string {
  const walk = (n: any): string => (n.text || '') + (n.children || []).map(walk).join('')
  return walk(root)
}

test('read original opens the cited comparison pane and transfers the verified locator by ticket', async t => {
  const left = { kb: 'kb-77', id: 'first-doc' }
  const right = { kb: 'kb-77', id: target.knowledgeId }
  const f = await fixture({
    target: { ...target, knowledgeBaseId: right.kb },
    details: { metadata: { content: citedMarkdown } },
    readerQuestion: JSON.stringify({ left, right }),
  })
  t.after(f.dispose)
  await f.openReader()
  assert.deepEqual(JSON.parse(JSON.stringify(f.navigations[0])), {
    name: 'document-reader', query: { kb: left.kb, doc: left.id, compare: right.id },
    state: { readerSource: true, readerSide: 'B', readerCitation: 'citation-ticket' },
  })
  assert.equal(f.citations[0][1], manualCitationFingerprint(new TextEncoder().encode(citedMarkdown).buffer))
  assert.equal(f.citations[0][2], f.preview().locate)
})

test('a refreshed chunk cannot silently replace the original fingerprint in an older citation', async t => {
  const f = await fixture({
    target: { ...target, locators: [{ type: 'docx', block: 3, source_hash: 'original-fingerprint' }] },
    details: { file_hash: 'new-fingerprint' },
    chunk: { source_locators: [{ type: 'docx', block: 1, source_hash: 'new-fingerprint' }] },
  })
  t.after(f.dispose)
  assert.equal(f.preview().locate.unavailable, true)
  assert.equal(f.preview().locate.quotes.length, 0)
})

test('citation header shows the source knowledge base name', async t => {
  // 多库/跨租户检索的溯源: 引用卡头部应显示被引文档所属知识库。
  const kbTarget = { ...target, knowledgeBaseId: 'kb-77' }
  const f = await fixture({ target: kbTarget }); t.after(f.dispose)
  await flush()
  assert.ok(nodeText(f.root).includes('测试知识库'), 'KB name line must render under the title')

  // 无 KB id 的引用(如手工知识)不应渲染空行
  const f2 = await fixture(); t.after(f2.dispose)
  await flush()
  assert.ok(!nodeText(f2.root).includes('测试知识库'), 'no KB id, no provenance line')
})
