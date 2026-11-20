import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import { effectScope, nextTick, reactive, shallowRef as ref, watch } from 'vue'
import { parse, compileTemplate } from '@vue/compiler-sfc'

const source = readFileSync(new URL('./document-preview.vue', import.meta.url), 'utf8')
const start = source.indexOf('const previewRoot =')
const end = source.indexOf('\nfunction toggleFullscreen()', start)
assert.ok(start >= 0 && end > start)
const focusSource = ts.transpile(source.slice(start, end))

async function flush() {
  await nextTick()
  await nextTick()
}

function setup(t, disableAutoFocus = false) {
  const scope = effectScope()
  t.after(() => scope.stop())
  const document = { body: {}, activeElement: null }
  document.activeElement = document.body
  const calls = []
  const element = name => ({
    isConnected: true,
    focus(options) {
      assert.equal(options.preventScroll, true)
      document.activeElement = this
      calls.push(name)
    },
  })
  const props = reactive({ active: true, sourceKey: 'artifact:one', disableAutoFocus })
  const docxContainer = ref(null)
  const htmlViewMode = ref('render')
  const isFullscreen = ref(false)
  const state = scope.run(() => vm.runInNewContext(`${focusSource}; ({
    previewRoot, previewContent, focusPreviewContent, onPreviewFrameLoad, toggleHtmlViewMode,
  })`, {
    ref, watch, nextTick, props, document, docxContainer, htmlViewMode, isFullscreen,
    getPreviewSourceKey: () => props.sourceKey,
  }))
  return { ...state, props, document, docxContainer, htmlViewMode, isFullscreen, calls, element }
}

test('opening a preview focuses its scroll container without scrolling ancestors', async t => {
  const ctx = setup(t)
  ctx.previewRoot.value = ctx.element('root')
  ctx.previewContent.value = ctx.element('markdown')
  await flush()
  assert.deepEqual(ctx.calls, ['markdown'])
})

test('delayed content inherits focus from the loading preview', async t => {
  const ctx = setup(t)
  ctx.previewRoot.value = ctx.element('root')
  await flush()
  assert.deepEqual(ctx.calls, ['root'])
  ctx.previewContent.value = ctx.element('code')
  await flush()
  assert.deepEqual(ctx.calls, ['root', 'code'])
})

test('loading does not steal focus from the composer or preview toolbar', async t => {
  for (const target of ['composer', 'toolbar']) {
    const ctx = setup(t)
    ctx.previewRoot.value = ctx.element('root')
    await flush()
    const control = ctx.element(target)
    ctx.document.activeElement = control
    ctx.previewContent.value = ctx.element('excel')
    await flush()
    assert.equal(ctx.document.activeElement, control)
    assert.deepEqual(ctx.calls, ['root'])
  }
})

test('hidden previews never take focus and reactivation focuses cached content', async t => {
  const ctx = setup(t)
  ctx.props.active = false
  ctx.previewRoot.value = ctx.element('root')
  ctx.previewContent.value = ctx.element('cached')
  await flush()
  ctx.focusPreviewContent()
  assert.deepEqual(ctx.calls, [])
  ctx.props.active = true
  await flush()
  assert.deepEqual(ctx.calls, ['cached'])
})

test('switching files, source mode and fullscreen restores content focus', async t => {
  const ctx = setup(t)
  ctx.previewRoot.value = ctx.element('root')
  ctx.previewContent.value = ctx.element('content')
  await flush()
  for (const update of [
    () => { ctx.props.sourceKey = 'artifact:two' },
    () => { ctx.htmlViewMode.value = 'source' },
    () => { ctx.isFullscreen.value = true },
    () => { ctx.isFullscreen.value = false },
  ]) {
    ctx.document.activeElement = ctx.element('button')
    update()
    await flush()
    assert.equal(ctx.document.activeElement, ctx.previewContent.value)
  }
})

test('DOCX focuses the renderer container and detached previews do not focus', async t => {
  const ctx = setup(t)
  ctx.previewRoot.value = ctx.element('root')
  await flush()
  ctx.docxContainer.value = ctx.element('docx')
  await flush()
  assert.deepEqual(ctx.calls, ['root', 'docx'])
  ctx.previewRoot.value.isConnected = false
  ctx.focusPreviewContent()
  assert.deepEqual(ctx.calls, ['root', 'docx'])
})

test('iframe load retains preview focus without taking it from another input', async t => {
  const ctx = setup(t)
  ctx.previewRoot.value = ctx.element('root')
  ctx.previewContent.value = ctx.element('iframe')
  await flush()
  ctx.onPreviewFrameLoad()
  assert.deepEqual(ctx.calls, ['iframe', 'iframe'])
  const input = ctx.element('input')
  ctx.document.activeElement = input
  ctx.onPreviewFrameLoad()
  assert.equal(ctx.document.activeElement, input)
})

test('inline relation previews load and switch files without moving focus from their host', async t => {
  const ctx = setup(t, true)
  const trigger = ctx.element('relation disclosure')
  ctx.document.activeElement = trigger
  ctx.previewRoot.value = ctx.element('root')
  await flush()
  assert.equal(ctx.document.activeElement, trigger)

  ctx.props.sourceKey = 'artifact:two'
  ctx.htmlViewMode.value = 'source'
  ctx.previewContent.value = ctx.element('iframe')
  await flush()
  ctx.onPreviewFrameLoad()
  assert.equal(ctx.document.activeElement, trigger)
  assert.deepEqual(ctx.calls, [])

  ctx.document.activeElement = ctx.previewRoot.value
  ctx.previewContent.value = ctx.element('rendered content')
  await flush()
  ctx.onPreviewFrameLoad()
  assert.equal(ctx.document.activeElement, ctx.previewRoot.value)
  assert.deepEqual(ctx.calls, [])
})

test('inline previews still focus on explicit HTML view and fullscreen changes', async t => {
  const ctx = setup(t, true)
  ctx.previewRoot.value = ctx.element('root')
  await flush()
  ctx.isFullscreen.value = true
  await flush()
  assert.deepEqual(ctx.calls, ['root'])
  ctx.previewContent.value = ctx.element('content')
  await flush()
  assert.deepEqual(ctx.calls, ['root', 'content'])

  for (const update of [() => { ctx.isFullscreen.value = false }, ctx.toggleHtmlViewMode]) {
    ctx.document.activeElement = ctx.element('toolbar button')
    update()
    await flush()
    assert.equal(ctx.document.activeElement, ctx.previewContent.value)
  }
  assert.deepEqual(ctx.calls, ['root', 'content', 'content', 'content'])
})

test('the inline wrapper loads an active preview with automatic focus disabled', () => {
  const wrapper = readFileSync(new URL('../views/business/ReadingRelationPreview.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(wrapper)
  assert.match(descriptor.template.content, /<DocumentPreview[^>]*\bactive disable-auto-focus\b/)
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: 'ReadingRelationPreview.vue', id: 'relation-preview' }).errors, [])
})

test('every document scroll target is tabbable and key events keep native behavior', () => {
  const { descriptor } = parse(source)
  const template = descriptor.template.content
  for (const tag of template.matchAll(/<[^>]+ref="(?:previewContent|docxContainer)"[^>]*>/g)) {
    assert.ok(/tabindex="0"|\bcontrols\b/.test(tag[0]), tag[0])
  }
  assert.doesNotMatch(template, /@key(?:down|up)/)
  assert.match(template, /sandbox="allow-scripts"/)
  assert.deepEqual(compileTemplate({ source: template, filename: 'document-preview.vue', id: 'preview' }).errors, [])
})
