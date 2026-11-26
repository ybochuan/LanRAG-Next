<template>
  <section class="zrimo-navigation" :aria-label="t(kind === 'thumbnails' ? 'reader.docx.thumbnails' : 'reader.docx.searchResults')">
    <div v-if="kind === 'thumbnails'" ref="thumbnails" class="zrimo-thumbnails">
      <t-button v-for="index in visiblePages" :key="index" variant="outline" size="small"
        class="zrimo-thumbnail" :aria-label="unitLabel(index)" :aria-current="isCurrent(index) ? 'page' : undefined"
        @click="emit('navigate', index)">
        <canvas :data-page="index" aria-hidden="true" />
        <span>{{ unitLabel(index) }}</span>
      </t-button>
    </div>
    <ul v-else class="zrimo-results">
      <li v-for="result in visibleResults" :key="result.pageIndex">
        <t-button variant="text" size="small" :aria-current="isCurrent(result.pageIndex) ? 'page' : undefined"
          @click="emit('navigate', result.pageIndex)">
          <strong>{{ unitLabel(result.pageIndex) }}</strong>
          <span :title="snippets[result.pageIndex]">{{ snippets[result.pageIndex] || result.text }}</span>
        </t-button>
      </li>
    </ul>
    <t-pagination v-if="total > batchSize" v-model="batch" :page-size="batchSize" :total="total"
      :show-page-size="false" :total-content="false" :max-page-btn="3" theme="simple" size="small" />
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DocumentInfo, DocumentViewer, SearchMatch } from '@zrimo/viewer'

const props = defineProps<{
  kind: 'thumbnails' | 'matches'
  viewer: DocumentViewer
  info: DocumentInfo
  pageIndex: number
  spread: boolean
  matches: readonly SearchMatch[]
}>()
const emit = defineEmits<{ navigate: [index: number] }>()
const { t } = useI18n()
const batchSize = 8
const batch = ref(1)
const thumbnails = ref<HTMLElement>()
const snippets = shallowRef<Record<number, string>>({})
const matchPages = computed(() => {
  const seen = new Map<number, SearchMatch>()
  for (const match of props.matches) {
    if (!seen.has(match.pageIndex)) seen.set(match.pageIndex, match)
  }
  return [...seen.values()]
})
const total = computed(() => props.kind === 'thumbnails' ? props.info.pageCount : matchPages.value.length)
const offset = computed(() => (batch.value - 1) * batchSize)
const visiblePages = computed(() => Array.from({ length: Math.max(0, Math.min(batchSize, total.value - offset.value)) },
  (_, index) => offset.value + index))
const visibleResults = computed(() => matchPages.value.slice(offset.value, offset.value + batchSize))

function unitLabel(index: number) {
  const page = index + 1
  if (props.info.unit === 'sheet') return props.info.sheetNames?.[index] ?? props.info.sheets?.[index]?.name ?? t('reader.docx.navigationPage', { page })
  if (props.info.unit === 'slide') return t('reader.docx.navigationSlide', { page })
  if (props.info.unit === 'image') return t('reader.docx.navigationImage', { page })
  return t('reader.docx.navigationPage', { page })
}

function isCurrent(index: number) {
  return index === props.pageIndex || (props.spread && index === props.pageIndex + 1)
}

if (props.kind === 'thumbnails') batch.value = Math.floor(props.pageIndex / batchSize) + 1

watch(() => [props.kind, offset.value, total.value, visibleResults.value] as const, async (_, __, onCleanup) => {
  const controller = new AbortController()
  const { signal } = controller
  onCleanup(() => controller.abort())
  snippets.value = {}
  await nextTick()
  if (signal.aborted) return
  if (props.kind === 'thumbnails') {
    for (const canvas of thumbnails.value?.querySelectorAll<HTMLCanvasElement>('canvas[data-page]') ?? []) {
      if (signal.aborted) break
      try {
        await props.viewer.renderThumbnail(Number(canvas.dataset.page), canvas, { maxWidth: 80, maxHeight: 72, devicePixelRatio: 1, signal })
        if (!signal.aborted) canvas.dataset.ready = ''
      } catch {
        // Page navigation remains available if a thumbnail cannot be drawn.
      }
    }
  } else {
    for (const match of visibleResults.value) {
      if (signal.aborted) break
      try {
        const text = await props.viewer.getPageText(match.pageIndex, signal)
        const start = Math.max(0, match.start - 42)
        const end = Math.min(text.length, match.end + 68)
        const excerpt = text.slice(start, end).replace(/\s+/gu, ' ').trim()
        if (!signal.aborted && excerpt) snippets.value = { ...snippets.value, [match.pageIndex]: `${start ? '…' : ''}${excerpt}${end < text.length ? '…' : ''}` }
      } catch {
        // Keep the original SDK match visible when context is unavailable.
      }
    }
  }
}, { flush: 'post', immediate: true })
</script>

<style scoped>
.zrimo-navigation { display: flex; flex-direction: column; gap: var(--td-comp-margin-xxs); max-height: min(34%, 10rem); min-height: 0; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); padding: var(--td-comp-paddingTB-xs) var(--td-comp-paddingLR-xs); }
.zrimo-thumbnails { display: flex; gap: var(--td-comp-margin-xs); align-items: flex-start; min-height: 0; overflow: auto; }
.zrimo-thumbnail { flex: none; width: 6rem; height: auto; min-height: var(--td-comp-size-s); white-space: normal; }
.zrimo-thumbnail :deep(.t-button__text) { display: grid; justify-items: center; gap: var(--td-comp-margin-xxs); padding-block: var(--td-comp-paddingTB-xs); }
.zrimo-thumbnail canvas { display: none; max-width: 100%; }
.zrimo-thumbnail canvas[data-ready] { display: block; }
.zrimo-navigation [aria-current="page"] { color: var(--td-brand-color); border-color: var(--td-brand-color); }
.zrimo-results { min-height: 0; overflow: auto; margin: 0; padding: 0; list-style: none; }
.zrimo-results li + li { border-top: 1px solid var(--td-component-stroke); }
.zrimo-results .t-button { width: 100%; justify-content: flex-start; text-align: start; }
.zrimo-results :deep(.t-button__text) { display: flex; align-items: baseline; gap: var(--td-comp-margin-xs); min-width: 0; }
.zrimo-results span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--td-text-color-secondary); }
</style>
