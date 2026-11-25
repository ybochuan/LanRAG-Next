<template>
  <t-card :title="title" :bordered="true">
    <div v-if="!upstream" class="pane-actions">
      <t-button size="small" :disabled="!ready" @click="viewer.previous()">上一页</t-button>
      <t-button size="small" :disabled="!ready" @click="viewer.next()">下一页</t-button>
      <t-button size="small" :disabled="!ready" @click="viewer.fitWidth()">适宽</t-button>
      <t-button size="small" :disabled="!ready" @click="viewer.zoomIn()">放大</t-button>
      <t-button size="small" :disabled="!ready" @click="viewer.zoomOut()">缩小</t-button>
      <t-input v-model="query" aria-label="页内查找" placeholder="页内查找" size="small" @enter="findText" />
      <t-button size="small" :disabled="!ready || !query.trim()" @click="findText">查找</t-button>
      <output v-if="match" :data-match-count="match.count" :data-match-page="match.page">
        {{ match.count }} 处匹配
      </output>
    </div>
    <t-alert v-if="error" theme="error" :message="error" />
    <div v-else class="pane-meta" role="status" :data-ready="ready" :data-page-count="state?.pageCount">
      {{ ready ? `${file.fileName} · ${state.pageIndex + 1} / ${state.pageCount}` : '正在渲染' }}
    </div>
    <div ref="viewport" class="viewport" />
  </t-card>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'

const props = defineProps({
  client: { type: Object, required: true },
  file: { type: Object, required: true },
  title: { type: String, required: true },
  upstream: Boolean,
})
const viewport = ref(null)
const state = shallowRef()
const error = ref('')
const query = ref('')
const match = shallowRef()
const ready = computed(() => state.value?.status === 'ready')
let viewer
let offState
let generation = 0
let pendingLoad
let pendingSearch

async function load(file) {
  const requested = ++generation
  pendingLoad?.abort()
  pendingSearch?.abort()
  pendingLoad = new AbortController()
  error.value = ''
  query.value = ''
  match.value = null
  try {
    await viewer.load(file.blob, { fileName: file.fileName, signal: pendingLoad.signal })
  } catch (cause) {
    if (requested === generation) error.value = cause.code || cause.message
  }
}

async function findText() {
  if (!ready.value || !query.value.trim()) return
  pendingSearch?.abort()
  const request = new AbortController()
  pendingSearch = request
  match.value = null
  try {
    const result = await viewer.search(query.value.trim())
    if (request.signal.aborted) return
    const hit = result.matches[0]
    match.value = { count: result.matches.length, page: hit?.pageIndex }
    if (hit) {
      viewer.goToPage(hit.pageIndex)
      await viewer.selectText({
        startPageIndex: hit.pageIndex,
        endPageIndex: hit.pageIndex,
        startOffset: hit.start,
        endOffset: hit.end,
      })
    }
  } catch (cause) {
    if (!request.signal.aborted) error.value = cause.code || cause.message
  }
}

onMounted(() => {
  viewer = props.client.createViewer({
    container: viewport.value,
    ui: props.upstream,
    fit: 'width',
    overscan: 1,
  })
  offState = viewer.on('statechange', next => { state.value = next })
  void load(props.file)
})
watch(() => props.file, file => { if (viewer) void load(file) })
onBeforeUnmount(() => {
  generation += 1
  pendingLoad?.abort()
  pendingSearch?.abort()
  offState?.()
  void viewer?.destroy()
})
</script>

<style scoped>
.pane-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.pane-actions :deep(.t-input__wrap) { width: 12rem; }
.pane-meta { margin: 8px 0; font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
</style>
