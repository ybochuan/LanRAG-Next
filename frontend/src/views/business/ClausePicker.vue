<template>
  <div class="clause-picker">
    <label>{{ label }}
      <t-select :value="modelValue?.chunk_id || ''" :disabled="disabled || !documentId || loading || hashing" :options="options" :placeholder="documentId ? '请选择条款分块' : '请先选择文档'" @change="choose" />
    </label>
    <pre v-if="selectedText" class="content-preview">{{ selectedText }}</pre>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="documentId && !loading && !rows.length" class="subtle">当前页没有可选正文分块。</p>
    <div v-if="documentId" class="mini-pages">
      <t-button variant="outline" size="small" :disabled="disabled || loading || page <= 1" @click="move(-1)">上一页</t-button>
      <span>{{ loading ? '正在载入…' : `分块第 ${page} 页` }}</span>
      <t-button variant="outline" size="small" :disabled="disabled || loading || !hasNext" @click="move(1)">下一页</t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { clauseAnchor, clauseChunks, type ClauseAnchor, type ClauseChunk } from '@/api/business'
import { requestFence } from '../lifecycle/model'

const props = defineProps<{ kb: string; documentId: string; label: string; modelValue?: ClauseAnchor; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value?: ClauseAnchor] }>()
const rows = ref<ClauseChunk[]>([])
const page = ref(1), hasNext = ref(false), loading = ref(false), hashing = ref(false)
const error = ref(''), selectedText = ref(''), selectedIndex = ref(0)
const listFence = requestFence(), selectionFence = requestFence()

async function load() {
  const token = listFence.begin()
  rows.value = []
  hasNext.value = false
  error.value = ''
  if (!props.documentId) {
    loading.value = false
    return
  }
  loading.value = true
  try {
    const result = await clauseChunks(props.kb, props.documentId, page.value)
    if (!listFence.current(token)) return
    rows.value = result.rows
    hasNext.value = result.hasNext
  } catch {
    if (listFence.current(token)) error.value = '无法载入文档分块。'
  } finally {
    if (listFence.current(token)) loading.value = false
  }
}

function move(delta: number) {
  page.value += delta
  void load()
}

const options = computed(() => {
  const items: { label: string; value: string }[] = rows.value.map(row => ({ label: `分块 ${row.chunk_index + 1} · ${row.content.replace(/\s+/g, ' ').slice(0, 64)}`, value: row.id }))
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
  page.value = 1
  void load()
}, { immediate: true })
onBeforeUnmount(() => {
  listFence.invalidate()
  selectionFence.invalidate()
})
</script>
