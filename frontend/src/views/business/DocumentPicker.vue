<template>
  <div class="document-picker">
    <label>{{ label }}
      <t-select
        :value="modelValue"
        :disabled="disabled || loading"
        :options="options"
        :filterable="searchable !== false"
        :loading="loading"
        :placeholder="loading ? '正在载入…' : '请选择生效文档'"
        @change="chooseValue"
      />
    </label>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="!loading && !items.length" class="subtle">当前知识库没有可选的生效文档。</p>
    <p v-else-if="!loading" class="subtle">共 {{ items.length }} 篇可选生效文档<span v-if="truncated">（已达 {{ items.length }} 篇上限）</span>；输入标题可全局筛选。</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { overview, type DocumentRow } from '@/api/lifecycle'
import { requestFence } from '../lifecycle/model'

const props = defineProps<{ kb: string; modelValue: string; label: string; disabled?: boolean; exclude?: string; searchable?: boolean; selectedTitle?: string; showIdentity?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [id: string] }>()

// The picker is a global search over the KB's active documents, not a page of
// them: pagination hid most candidates behind "next page" and made the title
// filter look page-scoped. Load every page once, then let the native filterable
// select narrow the complete set.
const PAGE_SIZE = 100
const MAX_DOCUMENTS = 2000

const items = ref<DocumentRow[]>([])
const loading = ref(false)
const error = ref('')
const truncated = ref(false)
const selectedName = ref('')
const fence = requestFence()

function displayTitle(title: string | undefined, id: string) {
  if (!title || title === id) return id
  return props.showIdentity ? `${title} · ${id.slice(0, 8)}` : title
}

async function load() {
  const token = fence.begin()
  const kb = props.kb
  loading.value = true
  error.value = ''
  truncated.value = false
  try {
    const rows: DocumentRow[] = []
    const maxPages = Math.ceil(MAX_DOCUMENTS / PAGE_SIZE)
    for (let page = 1; page <= maxPages; page++) {
      const data = await overview(kb, page, 'active', '', PAGE_SIZE)
      if (!fence.current(token)) return
      rows.push(...data.documents.filter(row => row.document_id !== props.exclude))
      if (!data.documents.length || rows.length >= data.total) break
      if (rows.length >= MAX_DOCUMENTS) {
        truncated.value = true
        break
      }
    }
    items.value = rows
  } catch {
    if (fence.current(token)) {
      items.value = []
      error.value = '无法载入生效文档。'
    }
  } finally {
    if (fence.current(token)) loading.value = false
  }
}

const options = computed(() => {
  const rows = items.value.map(item => ({ label: displayTitle(item.title, item.document_id), value: item.document_id }))
  if (props.modelValue && !rows.some(row => row.value === props.modelValue)) {
    rows.unshift({ label: displayTitle(props.selectedTitle || selectedName.value, props.modelValue), value: props.modelValue })
  }
  return [{ label: '请选择生效文档', value: '' }, ...rows]
})

function chooseValue(id: string) {
  selectedName.value = items.value.find(v => v.document_id === id)?.title || id
  emit('update:modelValue', id)
}

watch(() => [props.kb, props.exclude], () => {
  fence.invalidate()
  items.value = []
  selectedName.value = ''
  truncated.value = false
  if (props.modelValue === props.exclude) emit('update:modelValue', '')
  if (props.kb) void load()
  else loading.value = false
}, { immediate: true })

onBeforeUnmount(() => fence.invalidate())
</script>
