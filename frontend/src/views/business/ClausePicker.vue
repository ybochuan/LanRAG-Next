<template>
  <div class="clause-picker">
    <label>{{ label }}
      <select :value="modelValue?.chunk_id || ''" :disabled="disabled || !documentId || loading || hashing" required @change="choose">
        <option value="">{{ documentId ? '请选择条款分块' : '请先选择文档' }}</option>
        <option v-if="modelValue && !rows.some(row => row.id === modelValue?.chunk_id)" :value="modelValue.chunk_id">已选分块 {{ selectedIndex }}</option>
        <option v-for="row in rows" :key="row.id" :value="row.id">分块 {{ row.chunk_index + 1 }} · {{ row.content.replace(/\s+/g, ' ').slice(0, 64) }}</option>
      </select>
    </label>
    <pre v-if="selectedText" class="content-preview">{{ selectedText }}</pre>
    <p v-if="error" role="alert" class="notice error">{{ error }} <button type="button" @click="load">重试</button></p>
    <p v-else-if="documentId && !loading && !rows.length" class="subtle">当前页没有可选正文分块。</p>
    <div v-if="documentId" class="mini-pages">
      <button type="button" :disabled="disabled || loading || page <= 1" @click="move(-1)">上一页</button>
      <span>{{ loading ? '正在载入…' : `分块第 ${page} 页` }}</span>
      <button type="button" :disabled="disabled || loading || !hasNext" @click="move(1)">下一页</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
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

async function choose(event: Event) {
  const id = (event.target as HTMLSelectElement).value
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
