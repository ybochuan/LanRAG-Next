<template>
  <div class="document-picker">
    <label v-if="searchable">按标题搜索{{ label }}
      <t-input v-model="keyword" :maxlength="160" :disabled="disabled" placeholder="输入文档标题关键词，搜索全部生效文档" clearable />
    </label>
    <label>{{ label }}
      <t-select
        :value="modelValue"
        :disabled="disabled || loading"
        :options="options"
        :filterable="!searchable"
        :loading="loading"
        :placeholder="loading ? '正在载入…' : '请选择生效文档'"
        @change="chooseValue"
      />
    </label>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="!loading && !items.length" class="subtle">当前知识库没有可选的生效文档。</p>
    <p v-else-if="!loading" class="subtle">共 {{ items.length }} 篇可选生效文档<span v-if="truncated">（已达 {{ items.length }} 篇上限）</span><template v-if="keyword.trim()">，匹配 {{ filteredItems.length }} 篇</template>。</p>
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
const keyword = ref('')
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

// The keyword filters the whole loaded set, so it searches every active
// document in the KB instead of just the visible page.
const filteredItems = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return items.value
  return items.value.filter(item => `${item.title || ''} ${item.document_id}`.toLowerCase().includes(q))
})

const options = computed(() => {
  const rows = filteredItems.value.map(item => ({ label: displayTitle(item.title, item.document_id), value: item.document_id }))
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
  keyword.value = ''
  truncated.value = false
  if (props.modelValue === props.exclude) emit('update:modelValue', '')
  if (props.kb) void load()
  else loading.value = false
}, { immediate: true })

onBeforeUnmount(() => fence.invalidate())
</script>
