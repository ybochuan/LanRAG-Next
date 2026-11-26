<template>
  <div ref="root" class="zrimo-reader" :class="{ 'zrimo-reader--fullscreen': fullscreen }" tabindex="-1"
    :role="fullscreen ? 'dialog' : 'region'" :aria-modal="fullscreen || undefined" :aria-label="fileName"
    data-reader-engine="zrimo" @keydown="onReaderKeydown" @keydown.esc="onEscape">
    <div v-if="ready" class="zrimo-tools">
      <div class="zrimo-tool-group">
        <t-button v-if="ready.pageCount > 1" variant="text" size="small" shape="square"
          :disabled="!canPrevious" :aria-label="$t('reader.docx.previous')" :title="$t('reader.docx.previous')" @click="previousPage">
          <t-icon name="chevron-left" />
        </t-button>
        <span role="status" class="zrimo-page-status">{{ pageStatus }}</span>
        <t-select v-if="sheetOptions.length > 1" v-model="activeSheet" :options="sheetOptions" size="small"
          class="zrimo-sheet" :aria-label="$t('reader.docx.sheet')" @change="onSheetChange" />
        <t-button v-if="ready.pageCount > 1" variant="text" size="small" shape="square"
          :disabled="!canNext" :aria-label="$t('reader.docx.next')" :title="$t('reader.docx.next')" @click="nextPage">
          <t-icon name="chevron-right" />
        </t-button>
        <t-button v-if="canSpread" variant="text" size="small" shape="square" :aria-pressed="spread"
          :aria-label="$t(spread ? 'reader.docx.singlePage' : 'reader.docx.spread')"
          :title="$t(spread ? 'reader.docx.singlePage' : 'reader.docx.spread')" @click="toggleSpread">
          <t-icon :name="spread ? 'view-list' : 'view-module'" />
        </t-button>
      </div>
      <div v-if="!spread" class="zrimo-tool-group">
        <t-button variant="text" size="small" shape="square" :aria-label="$t('preview.zoomOut')" :title="$t('preview.zoomOut')" @click="viewer?.zoomOut()">
          <t-icon name="zoom-out" />
        </t-button>
        <span class="zrimo-zoom">{{ zoomRatio }}</span>
        <t-button variant="text" size="small" shape="square" :aria-label="$t('preview.zoomIn')" :title="$t('preview.zoomIn')" @click="viewer?.zoomIn()">
          <t-icon name="zoom-in" />
        </t-button>
        <t-button variant="text" size="small" :aria-pressed="ready.fit === 'width'" @click="viewer?.fitWidth()">{{ $t('reader.docx.fitWidth') }}</t-button>
        <t-button variant="text" size="small" :aria-pressed="ready.fit === 'page'" @click="viewer?.fitPage()">{{ $t('reader.docx.fitPage') }}</t-button>
      </div>
      <div class="zrimo-tool-group zrimo-tools-end">
        <t-button v-if="canThumbnail" variant="text" size="small" shape="square" class="zrimo-thumbnails-toggle"
          :aria-expanded="navigation === 'thumbnails'" :aria-label="$t('reader.docx.thumbnails')"
          :title="$t('reader.docx.thumbnails')" @click="navigation = navigation === 'thumbnails' ? '' : 'thumbnails'">
          <t-icon name="image" />
        </t-button>
        <t-button v-if="canSearch && !spread" variant="text" size="small" shape="square" class="zrimo-search-toggle"
          :aria-expanded="searchOpen" :aria-label="$t('reader.docx.search')" :title="$t('reader.docx.search')"
          aria-keyshortcuts="Control+F Meta+F" @click="searchOpen ? closeSearch() : openSearch()">
          <t-icon name="search" />
        </t-button>
        <t-button variant="text" size="small" shape="square" :aria-pressed="fullscreen" :disabled="!fullscreenOwner"
          :class="{ 'zrimo-fullscreen--inactive': !fullscreenOwner }"
          :aria-label="$t(fullscreen ? 'preview.exitFullscreen' : 'preview.fullscreen')"
          :title="$t(fullscreen ? 'preview.exitFullscreen' : 'preview.fullscreen')" @click="toggleFullscreen">
          <t-icon :name="fullscreen ? 'fullscreen-exit' : 'fullscreen'" />
        </t-button>
      </div>
    </div>
    <div v-if="ready && searchOpen" ref="searchBar" class="zrimo-find" role="search" :aria-label="$t('reader.docx.search')">
      <t-input v-model="query" clearable :maxlength="160" size="small" class="zrimo-query"
        :aria-label="$t('reader.docx.search')" :placeholder="$t('reader.docx.search')" @enter="onSearchEnter" />
      <t-button variant="text" size="small" shape="square" :loading="searching" :disabled="!query.trim()"
        :aria-label="$t('reader.docx.runSearch')" :title="$t('reader.docx.runSearch')" @click="findText">
        <t-icon name="search" />
      </t-button>
      <template v-if="matches?.matches.length">
        <t-button variant="text" size="small" shape="square" :aria-label="$t('reader.docx.previousMatch')" :title="$t('reader.docx.previousMatch')" @click="viewer?.searchPrevious()">
          <t-icon name="chevron-up" />
        </t-button>
        <t-button variant="text" size="small" shape="square" :aria-label="$t('reader.docx.nextMatch')" :title="$t('reader.docx.nextMatch')" @click="viewer?.searchNext()">
          <t-icon name="chevron-down" />
        </t-button>
        <t-button variant="text" size="small" shape="square" :aria-expanded="navigation === 'matches'"
          :aria-label="$t('reader.docx.searchResults')" :title="$t('reader.docx.searchResults')"
          @click="navigation = navigation === 'matches' ? '' : 'matches'">
          <t-icon name="view-list" />
        </t-button>
      </template>
      <span v-if="matches" role="status" class="zrimo-match-status">
        {{ matches.matches.length ? $t('reader.docx.matches', { current: matches.activeIndex + 1, total: matches.matches.length }) : $t('reader.docx.noMatches') }}
      </span>
      <t-button variant="text" size="small" shape="square" :aria-label="$t('reader.docx.closeSearch')" :title="$t('reader.docx.closeSearch')" @click="closeSearch">
        <t-icon name="close" />
      </t-button>
    </div>
    <ZrimoNavigation v-if="ready && info && viewer && navigation && (navigation !== 'matches' || matches?.matches.length)"
      :key="navigation === 'matches' ? `matches:${matches?.query}` : 'thumbnails'" :kind="navigation" :viewer="viewer" :info="info"
      :page-index="spread ? spreadStart : ready.pageIndex" :spread="spread" :matches="navigation === 'matches' ? matchItems : emptyMatches"
      @navigate="navigateTo" />
    <div v-if="selectionLocation && !spread" class="zrimo-selection">
      <span :title="selectionLocation">{{ selectionLocation }}</span>
      <t-button size="small" variant="outline" @mousedown.prevent @click="askSelection">
        {{ $t('reader.docx.askSelection') }}
      </t-button>
    </div>
    <t-alert v-if="searchFailed" theme="warning">{{ $t('reader.docx.searchFailed') }}</t-alert>
    <div v-if="!ready" role="status"><t-loading size="small" /> {{ $t('preview.loading') }}</div>
    <div v-show="!spread" ref="viewport" class="zrimo-viewport" />
    <div v-show="spread" ref="spreadViewport" class="zrimo-spread" :aria-label="$t('reader.docx.spread')">
      <div class="zrimo-spread__pages" :class="{ 'zrimo-spread__pages--single': !spreadHasSecond }">
        <canvas ref="spreadLeft" class="zrimo-spread__page" />
        <canvas v-show="spreadHasSecond" ref="spreadRight" class="zrimo-spread__page" />
      </div>
      <t-loading v-if="spreadBusy" class="zrimo-spread__loading" size="small" />
      <t-alert v-if="spreadError" theme="warning">{{ $t('reader.docx.spreadFailed') }}</t-alert>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CellRange, DocumentFormat, DocumentInfo, DocumentViewer, SearchMatch, SearchResult, ViewerClient, ViewerErrorCode, ViewerEventMap, ViewerState } from '@zrimo/viewer'
import { READER_ZRIMO_MAX_BYTES } from './readerApi'
import ZrimoNavigation from './ZrimoNavigation.vue'

const props = defineProps<{ blob: Blob; fileName: string; format?: string; fullscreenOwner: boolean }>()
const emit = defineEmits<{ issue: [reason: 'fallback' | 'tooLarge' | 'encrypted']; ask: [location: string] }>()
const root = ref<HTMLElement>()
const viewport = ref<HTMLElement>()
const spreadViewport = ref<HTMLElement>()
const spreadLeft = ref<HTMLCanvasElement>()
const spreadRight = ref<HTMLCanvasElement>()
const searchBar = ref<HTMLElement>()
const state = shallowRef<ViewerState>()
const info = shallowRef<DocumentInfo>()
const activeSheet = ref(0)
const query = ref('')
const matches = shallowRef<SearchResult | null>(null)
const searching = ref(false)
const searchFailed = ref(false)
const canSearch = ref(false)
const searchOpen = ref(false)
const navigation = ref<'' | 'thumbnails' | 'matches'>('')
const selection = shallowRef<ViewerEventMap['selectionchange']>(null)
const emptyMatches: readonly SearchMatch[] = []
const matchItems = computed(() => matches.value?.matches ?? emptyMatches)
const fullscreen = ref(false)
const spread = ref(false)
const spreadStart = ref(0)
const spreadBusy = ref(false)
const spreadError = ref(false)
const ready = computed(() => state.value?.status === 'ready' ? state.value : undefined)
const { t, locale } = useI18n()
const zoomFormatter = computed(() => new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 0 }))
const zoomRatio = computed(() => ready.value ? zoomFormatter.value.format(ready.value.zoom) : '')
const sheetOptions = computed(() => {
  const names = info.value?.sheetNames ?? info.value?.sheets?.map(sheet => sheet.name) ?? []
  return names.map((label, value) => ({ label, value }))
})
const canSpread = computed(() => !!ready.value && (info.value?.unit === 'page' || info.value?.unit === 'slide') && ready.value.pageCount > 1)
const canThumbnail = computed(() => (ready.value?.pageCount ?? 0) > 1 && !!info.value?.capabilities?.thumbnails)
const selectionLocation = computed(() => {
  const current = selection.value
  if (!current) return ''
  if ('text' in current) {
    if (!current.text.trim()) return ''
    const start = current.pageIndex + 1
    const end = (current.endPageIndex ?? current.pageIndex) + 1
    if (start !== end) return t('reader.docx.selectedPages', { start, end })
    const page = t(info.value?.unit === 'slide' ? 'reader.docx.navigationSlide' : 'reader.docx.navigationPage', { page: start })
    if (current.startOffset === undefined || current.endOffset === undefined) return page
    return t('reader.docx.selectedTextRange', { page, start: current.startOffset + 1, end: current.endOffset })
  }
  const ranges = 'ranges' in current ? current.ranges : [current]
  const sheet = sheetOptions.value[current.sheetIndex]?.label ?? t('reader.docx.sheet')
  return ranges.length ? t('reader.docx.selectedCells', { sheet, range: ranges.map(cellRangeLabel).join(', ') }) : ''
})
const spreadHasSecond = computed(() => !!ready.value && spreadStart.value + 1 < ready.value.pageCount)
const canPrevious = computed(() => !!ready.value && (spread.value ? spreadStart.value > 0 : ready.value.pageIndex > 0))
const canNext = computed(() => !!ready.value && (spread.value ? spreadStart.value + 2 < ready.value.pageCount : ready.value.pageIndex + 1 < ready.value.pageCount))
const pageStatus = computed(() => {
  if (!ready.value) return ''
  if (!spread.value) return t('reader.docx.page', { page: ready.value.pageIndex + 1, total: ready.value.pageCount })
  return t('reader.docx.spreadPage', {
    start: spreadStart.value + 1,
    end: Math.min(spreadStart.value + 2, ready.value.pageCount),
    total: ready.value.pageCount,
  })
})
const controller = new AbortController()
let client: ViewerClient | undefined
let viewer: DocumentViewer | undefined
let viewportResize: ResizeObserver | undefined
let spreadResize: ResizeObserver | undefined
let spreadRenderToken = 0
let reported = false
let trigger: HTMLElement | undefined
let nativeFullscreen = false
let previousOverflow: string | undefined
const unsubscribe: (() => void)[] = []

function report(code?: ViewerErrorCode) {
  if (controller.signal.aborted || reported) return
  reported = true
  emit('issue', code === 'resource-limit' ? 'tooLarge' : code === 'encrypted-document' ? 'encrypted' : 'fallback')
}

watch(query, () => {
  searchFailed.value = false
  viewer?.clearSearch()
  if (navigation.value === 'matches') navigation.value = ''
})

function cellRangeLabel(range: CellRange) {
  const cell = (row: number, column: number) => {
    let letters = ''
    while (column > 0) {
      column--
      letters = String.fromCharCode(65 + (column % 26)) + letters
      column = Math.floor(column / 26)
    }
    return `${letters}${row}`
  }
  const start = cell(range.startRow, range.startColumn)
  const end = cell(range.endRow, range.endColumn)
  return start === end ? start : `${start}:${end}`
}

function askSelection() {
  if (selectionLocation.value && !spread.value) emit('ask', selectionLocation.value)
}

function hasViewportTextSelection() {
  const current = document.getSelection()
  return !!current && !current.isCollapsed && !!viewport.value?.contains(current.anchorNode) && viewport.value.contains(current.focusNode)
}

function clearInactiveTextSelection() {
  if (selection.value && 'text' in selection.value && !hasViewportTextSelection()) selection.value = null
}

function navigateTo(index: number) {
  if (spread.value) void setSpreadStart(Math.floor(index / 2) * 2)
  else viewer?.goToPage(index)
}

async function openSearch() {
  if (spread.value) await toggleSpread()
  searchOpen.value = true
  await nextTick()
  searchBar.value?.querySelector('input')?.focus()
}

function closeSearch() {
  searchOpen.value = false
  query.value = ''
  if (navigation.value === 'matches') navigation.value = ''
  searchFailed.value = false
  root.value?.querySelector<HTMLElement>('.zrimo-search-toggle')?.focus()
}

function onReaderKeydown(event: KeyboardEvent) {
  if (!ready.value || !canSearch.value || !(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== 'f') return
  event.preventDefault()
  void openSearch()
}

function onSearchEnter(_value: string, { e }: { e: KeyboardEvent }) {
  if (e.isComposing) return
  if (matches.value?.matches.length) {
    if (e.shiftKey) viewer?.searchPrevious()
    else viewer?.searchNext()
  } else void findText()
}

async function findText() {
  const text = query.value.trim()
  if (!viewer || !ready.value || !text || searching.value) return
  searchFailed.value = false
  searching.value = true
  try {
    const result = await viewer.search(text)
    if (text !== query.value.trim()) viewer.clearSearch()
    else if (result.matches.length) navigation.value = 'matches'
  } catch {
    if (!controller.signal.aborted && text === query.value.trim()) searchFailed.value = true
  } finally {
    searching.value = false
  }
}

function previousPage() {
  if (spread.value) { void setSpreadStart(spreadStart.value - 2); return }
  viewer?.previous()
}

function nextPage() {
  if (spread.value) { void setSpreadStart(spreadStart.value + 2); return }
  viewer?.next()
}

async function renderSpread() {
  const current = ready.value
  const instance = viewer
  if (!spread.value || !current || !instance || !spreadLeft.value) return
  const token = ++spreadRenderToken
  spreadBusy.value = true
  spreadError.value = false
  try {
    const width = spreadViewport.value?.clientWidth ?? 0
    const page = info.value?.pageSizes?.[spreadStart.value]
    const pageWidth = page?.width ?? 794
    const columns = spreadHasSecond.value ? 2 : 1
    const available = Math.max(1, width - 32)
    const zoom = Math.max(0.1, Math.min(4, available / columns / pageWidth))
    const options = {
      zoom,
      devicePixelRatio: window.devicePixelRatio || 1,
      priority: 'visible' as const,
    }
    await instance.renderPage(spreadStart.value, spreadLeft.value, options)
    if (token !== spreadRenderToken) return
    if (spreadHasSecond.value && spreadRight.value) {
      await instance.renderPage(spreadStart.value + 1, spreadRight.value, options)
    }
  } catch {
    if (token === spreadRenderToken) spreadError.value = true
  } finally {
    if (token === spreadRenderToken) spreadBusy.value = false
  }
}

async function setSpreadStart(page: number) {
  const count = ready.value?.pageCount ?? 0
  if (!count) return
  spreadStart.value = Math.max(0, Math.min(page, count - 1))
  await renderSpread()
}

async function toggleSpread() {
  if (!canSpread.value) return
  if (!spread.value) {
    if (searchOpen.value) closeSearch()
    viewer?.clearSelection()
    spreadStart.value = Math.floor((ready.value?.pageIndex ?? 0) / 2) * 2
    spread.value = true
    await nextTick()
    if (spreadViewport.value && !spreadResize) {
      spreadResize = new ResizeObserver(() => { if (spread.value) void renderSpread() })
      spreadResize.observe(spreadViewport.value)
    }
    await renderSpread()
    return
  }
  const page = spreadStart.value
  spread.value = false
  spreadRenderToken += 1
  spreadBusy.value = false
  spreadResize?.disconnect()
  spreadResize = undefined
  await nextTick()
  viewer?.goToPage(page)
  fitToViewport()
}

function closeFullscreen(restore = true) {
  if (!fullscreen.value) return
  fullscreen.value = false
  nativeFullscreen = false
  if (previousOverflow !== undefined) document.body.style.overflow = previousOverflow
  previousOverflow = undefined
  if (root.value && document.fullscreenElement === root.value) void document.exitFullscreen().catch(() => {})
  if (restore && trigger?.isConnected) trigger.focus()
  trigger = undefined
}

async function toggleFullscreen(event: MouseEvent) {
  if (fullscreen.value) { closeFullscreen(); return }
  const element = root.value
  if (!element || !props.fullscreenOwner) return
  trigger = event.currentTarget instanceof HTMLElement ? event.currentTarget : undefined
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  fullscreen.value = true
  element.focus({ preventScroll: true })
  try {
    await element.requestFullscreen()
    if (!fullscreen.value || controller.signal.aborted) {
      if (document.fullscreenElement === element) await document.exitFullscreen()
    } else {
      nativeFullscreen = true
    }
  } catch {
    // The pane's overlay also works in desktop hosts that decline native fullscreen.
  }
}

function onFullscreenChange() {
  if (nativeFullscreen && document.fullscreenElement !== root.value) closeFullscreen()
}

function onEscape(event: KeyboardEvent) {
  const inNavigation = event.target instanceof Element && event.target.closest('.zrimo-navigation')
  if (searchOpen.value && ((event.target instanceof Node && searchBar.value?.contains(event.target)) ||
    (inNavigation && navigation.value === 'matches'))) {
    event.preventDefault()
    event.stopPropagation()
    closeSearch()
    return
  }
  if (inNavigation && navigation.value === 'thumbnails') {
    event.preventDefault()
    event.stopPropagation()
    navigation.value = ''
    root.value?.querySelector<HTMLElement>('.zrimo-thumbnails-toggle')?.focus()
    return
  }
  if (!fullscreen.value) return
  event.preventDefault()
  event.stopPropagation()
  closeFullscreen()
}

function keepFullscreenFocus(event: FocusEvent) {
  if (fullscreen.value && event.target instanceof Node && !root.value?.contains(event.target)) root.value?.focus()
}

function onSheetChange(value: string | number | boolean) {
  const index = Number(value)
  if (Number.isInteger(index) && index >= 0) {
    activeSheet.value = index
    viewer?.setSheet(index)
  }
}

function fitToViewport() {
  const fit = ready.value?.fit
  if ((fit === 'width' || fit === 'page') && viewport.value?.clientHeight && viewport.value.clientWidth) viewer?.setFit(fit)
}

watch(() => props.fullscreenOwner, owner => { if (!owner) closeFullscreen(false) })

onMounted(async () => {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('focusin', keepFullscreenFocus)
  document.addEventListener('selectionchange', clearInactiveTextSelection)
  try {
    const { ViewerClient, ViewerError } = await import('@zrimo/viewer')
    if (controller.signal.aborted || !viewport.value) return
    const base = new URL(import.meta.env.BASE_URL, document.baseURI)
    client = ViewerClient.create({
      assetBaseUrl: new URL('vendor/zrimo/', base),
      limits: { maxInputBytes: READER_ZRIMO_MAX_BYTES, maxConcurrentRenders: 1 },
      fontPolicy: { mode: 'auto' },
    })
    const container = viewport.value
    // Continuous keeps every page/slide in one scroll surface; Zrimo defaults
    // to it, but stating it here prevents a host default from becoming single.
    viewer = client.createViewer({ container, ui: false, fit: 'width', layout: 'continuous', overscan: 1 })
    unsubscribe.push(
      viewer.on('statechange', value => {
        state.value = value
        if (info.value?.unit === 'sheet') activeSheet.value = value.pageIndex
      }),
      viewer.on('error', error => report(error.code)),
      viewer.on('searchchange', result => {
        if (!result || result.query === query.value.trim()) matches.value = result
      }),
      viewer.on('selectionchange', value => {
        selection.value = value && 'text' in value && !hasViewportTextSelection() ? null : value
      }),
    )
    try {
      await viewer.load(props.blob, { fileName: props.fileName, format: props.format as DocumentFormat | undefined, signal: controller.signal })
      if (!controller.signal.aborted) {
        info.value = viewer.getDocumentInfo()
        activeSheet.value = 0
        canSearch.value = info.value.capabilities?.search === true
        await nextTick()
        if (!controller.signal.aborted) {
          fitToViewport()
          viewportResize = new ResizeObserver(fitToViewport)
          viewportResize.observe(container)
        }
      }
    } catch (error) {
      report(error instanceof ViewerError ? error.code : undefined)
    }
  } catch {
    report()
  }
})

onBeforeUnmount(() => {
  controller.abort()
  viewportResize?.disconnect()
  spreadResize?.disconnect()
  spreadRenderToken += 1
  closeFullscreen(false)
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('focusin', keepFullscreenFocus)
  document.removeEventListener('selectionchange', clearInactiveTextSelection)
  unsubscribe.forEach(stop => stop())
  void client?.destroy().catch(() => {})
})
</script>

<style scoped>
.zrimo-reader { position: relative; display: flex; flex: 1; flex-direction: column; gap: var(--td-comp-margin-s); min-width: 0; min-height: 0; }
.zrimo-tools, .zrimo-tool-group, .zrimo-find { display: flex; align-items: center; flex-wrap: wrap; gap: var(--td-comp-margin-xxs); }
.zrimo-tools { column-gap: var(--td-comp-margin-xs); }
.zrimo-tools-end { margin-inline-start: auto; }
/* Reserve the slot so activating a pane doesn't move its other controls mid-click. */
.zrimo-fullscreen--inactive { visibility: hidden; }
.zrimo-page-status, .zrimo-match-status, .zrimo-zoom { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.zrimo-zoom { font-variant-numeric: tabular-nums; text-align: center; min-width: 3em; }
.zrimo-query { flex: 1 1 8rem; min-width: 0; max-width: 16rem; }
.zrimo-sheet { width: 9rem; }
.zrimo-selection { position: absolute; z-index: 2; inset-inline-end: var(--td-comp-margin-s); bottom: var(--td-comp-margin-s); display: flex; align-items: center; gap: var(--td-comp-margin-xs); max-width: calc(100% - 2 * var(--td-comp-margin-s)); padding: var(--td-comp-paddingTB-xs) var(--td-comp-paddingLR-xs); border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.zrimo-selection span { min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.zrimo-selection .t-button { flex: none; }
.zrimo-viewport { flex: 1 1 0; min-height: 0; min-width: 0; overflow: hidden; }
.zrimo-spread { position: relative; flex: 1 1 0; min-height: 0; min-width: 0; overflow: auto; background: var(--td-bg-color-page); }
.zrimo-spread__pages { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; align-items: start; padding: 16px; min-width: 0; }
.zrimo-spread__pages--single { grid-template-columns: minmax(0, 1fr); }
.zrimo-spread__page { display: block; width: 100%; height: auto; background: #fff; box-shadow: 0 2px 8px rgb(16 24 40 / 18%); }
.zrimo-spread__loading { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; pointer-events: none; }
.zrimo-spread .t-alert { margin: 0 16px 16px; }
.zrimo-reader--fullscreen { position: fixed; inset: 0; z-index: 4000; width: 100%; height: 100%; box-sizing: border-box; background: var(--td-bg-color-container); padding: var(--td-comp-paddingTB-m) var(--td-comp-paddingLR-m); }
</style>
