<template>
  <div class="clause-picker">
    <label>按内容搜索{{ label }}
      <t-input v-model="keyword" :maxlength="160" :disabled="disabled || !documentId" placeholder="输入条款关键词，搜索全部正文分块" clearable />
    </label>
    <label>{{ label }}
      <t-select :value="modelValue?.chunk_id || ''" :disabled="disabled || !documentId || loading || hashing" :options="options" :placeholder="documentId ? '请选择条款分块' : '请先选择文档'" @change="choose" />
    </label>
    <pre v-if="selectedText" class="content-preview">{{ selectedText }}</pre>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="documentId && !loading && !rows.length" class="subtle">该文档没有可选正文分块。</p>
    <p v-else-if="documentId && !loading" class="subtle">共 {{ rows.length }} 个分块<template v-if="keyword.trim()">，匹配 {{ filteredRows.length }} 个</template>。</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { clauseAnchor, clauseChunks, type ClauseAnchor, type ClauseChunk } from '@/api/business'
import { requestFence } from '../lifecycle/model'

const props = defineProps<{ kb: string; documentId: string; label: string; modelValue?: ClauseAnchor; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value?: ClauseAnchor] }>()

// Same rule as the document picker: load the whole chunk set once and filter
// globally, so a keyword does not silently search only the current page.
const MAX_CHUNKS = 5000
const MAX_PAGES = 200

const rows = ref<ClauseChunk[]>([])
const keyword = ref('')
const loading = ref(false)
const hashing = ref(false)
const error = ref('')
const selectedText = ref('')
const selectedIndex = ref(0)
const listFence = requestFence(), selectionFence = requestFence()

async function load() {
  const token = listFence.begin()
  rows.value = []
  error.value = ''
  if (!props.documentId) {
    loading.value = false
    return
  }
  loading.value = true
  try {
    const collected: ClauseChunk[] = []
    for (let page = 1; page <= MAX_PAGES; page++) {
      const result = await clauseChunks(props.kb, props.documentId, page)
      if (!listFence.current(token)) return
      collected.push(...result.rows)
      if (!result.hasNext || !result.rows.length || collected.length >= MAX_CHUNKS) break
    }
    rows.value = collected
  } catch {
    if (listFence.current(token)) error.value = '无法载入文档分块。'
  } finally {
    if (listFence.current(token)) loading.value = false
  }
}

const filteredRows = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return rows.value
  return rows.value.filter(row => row.content.toLowerCase().includes(q))
})

const options = computed(() => {
  const items: { label: string; value: string }[] = filteredRows.value.map(row => ({
    label: `分块 ${row.chunk_index + 1} · ${row.content.replace(/\s+/g, ' ').slice(0, 64)}`,
    value: row.id,
  }))
  if (props.modelValue && !items.some(item => item.value === props.modelValue?.chunk_id)) {
    items.unshift({ label: `已选分块 ${selectedIndex.value}`, value: props.modelValue.chunk_id })
  }
  return [{ label: props.documentId ? '请选择条款分块' : '请先选择文档', value: '' }, ...items]
})

async function choose(id: string) {
  if (id === props.modelValue?.chunk_id) return
  const token = selectionFence.begin()
  emit('update:modelValue', undefined)
  selectedText.value = ''
  error.value = ''
  const chunk = rows.value.find(row => row.id === id)
  if (!chunk) return
  hashing.value = true
  try {
    const anchor = await clauseAnchor(chunk)
    if (!selectionFence.current(token)) return
    emit('update:modelValue', anchor)
    selectedText.value = chunk.content
    selectedIndex.value = chunk.chunk_index + 1
  } catch {
    if (selectionFence.current(token)) error.value = '无法核对条款指纹。请通过本机入口或 HTTPS 访问。'
  } finally {
    if (selectionFence.current(token)) hashing.value = false
  }
}

watch(() => [props.kb, props.documentId], () => {
  selectionFence.invalidate()
  hashing.value = false
  emit('update:modelValue', undefined)
  selectedText.value = ''
  keyword.value = ''
  void load()
}, { immediate: true })

onBeforeUnmount(() => {
  listFence.invalidate()
  selectionFence.invalidate()
})
</script>
