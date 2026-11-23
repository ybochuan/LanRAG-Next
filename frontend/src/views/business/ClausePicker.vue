<template>
  <div class="clause-picker">
    <label>{{ $t('workbench.picker.searchByContent') }}{{ label }}
      <t-input v-model="keyword" :maxlength="160" :disabled="disabled || !documentId" :placeholder="$t('workbench.picker.contentPlaceholder')" clearable />
    </label>
    <label>{{ label }}
      <t-select :value="modelValue?.chunk_id || ''" :disabled="disabled || !documentId || loading || hashing" :options="options" :placeholder="documentId ? $t('workbench.picker.selectClause') : $t('workbench.picker.selectDocFirst')" @change="choose" />
    </label>
    <pre v-if="selectedText" class="content-preview">{{ selectedText }}</pre>
    <t-alert v-if="error" theme="error">{{ error }} <t-button variant="text" size="small" @click="load">{{ $t('workbench.common.retry') }}</t-button></t-alert>
    <p v-else-if="documentId && !loading && !rows.length" class="subtle">{{ $t('workbench.picker.noChunks') }}</p>
    <p v-else-if="documentId && !loading" class="subtle">{{ keyword.trim() ? $t('workbench.businessUi.clause.matchingChunks',{n:rows.length,m:filteredRows.length}) : $t('workbench.businessUi.clause.totalChunks',{n:rows.length}) }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { clauseAnchor, clauseChunks, type ClauseAnchor, type ClauseChunk } from '@/api/business'
import { requestFence } from '../lifecycle/model'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ kb: string; documentId: string; label: string; modelValue?: ClauseAnchor; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value?: ClauseAnchor] }>()
const { t } = useI18n()

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
    if (listFence.current(token)) error.value = t('workbench.businessUi.clause.loadFailed')
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
    label: t('workbench.businessUi.clause.chunkOption',{index:row.chunk_index+1,text:row.content.replace(/\s+/g, ' ').slice(0, 64)}),
    value: row.id,
  }))
  if (props.modelValue && !items.some(item => item.value === props.modelValue?.chunk_id)) {
    items.unshift({ label: t('workbench.businessUi.clause.selectedChunk',{index:selectedIndex.value}), value: props.modelValue.chunk_id })
  }
  return [{ label: props.documentId ? t('workbench.picker.selectClause') : t('workbench.picker.selectDocFirst'), value: '' }, ...items]
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
    if (selectionFence.current(token)) error.value = t('workbench.businessUi.clause.fingerprintFailed')
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
