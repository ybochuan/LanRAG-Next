<template>
  <div class="document-picker">
    <label v-if="searchable">{{ $t('workbench.picker.searchByTitle') }}{{ label }}
      <t-input v-model="keyword" :maxlength="160" :disabled="disabled" :placeholder="$t('workbench.picker.titlePlaceholder')" clearable />
    </label>
    <label>{{ label }}
      <t-select
        :value="modelValue"
        :disabled="disabled || loading"
        :options="options"
        :filterable="!searchable"
        :loading="loading"
        :placeholder="loading ? $t('workbench.picker.loading') : $t('workbench.picker.selectDocument')"
        @change="chooseValue"
      />
    </label>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="!loading && !items.length" class="subtle">{{ $t('workbench.picker.noDocuments') }}</p>
    <div v-if="serverMode && loading && keyword.trim()" class="wb-loading" role="status"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.picker.searching') }}</span></div>
    <p v-else-if="!loading" class="subtle"><template v-if="serverMode">{{ $t('workbench.picker.serverHint', { total, shown: items.length }) }}</template><template v-else>{{ $t('workbench.picker.totalAll', { n: items.length }) }}</template></p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { overview, type DocumentRow } from '@/api/lifecycle'
import { requestFence } from '../lifecycle/model'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ kb: string; modelValue: string; label: string; disabled?: boolean; exclude?: string; searchable?: boolean; selectedTitle?: string; showIdentity?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [id: string] }>()
const { t } = useI18n()

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
const total = ref(0)
const serverMode = ref(false)
const SERVER_THRESHOLD = 300
let searchTimer: ReturnType<typeof setTimeout> | undefined
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
  serverMode.value = false
  try {
    const first = await overview(kb, 1, 'active', '', PAGE_SIZE)
    if (!fence.current(token)) return
    total.value = first.total
    if (first.total > SERVER_THRESHOLD) {
      // A large KB: keep the initial list short and let the keyword query the
      // server instead of rendering thousands of options.
      serverMode.value = true
      items.value = first.documents.filter(row => row.document_id !== props.exclude)
      return
    }
    const rows: DocumentRow[] = first.documents.filter(row => row.document_id !== props.exclude)
    let fetched = first.documents.length
    const maxPages = Math.ceil(MAX_DOCUMENTS / PAGE_SIZE)
    for (let page = 2; page <= maxPages && fetched < first.total; page++) {
      const data = await overview(kb, page, 'active', '', PAGE_SIZE)
      if (!fence.current(token)) return
      fetched += data.documents.length
      rows.push(...data.documents.filter(row => row.document_id !== props.exclude))
      if (!data.documents.length) break
      if (fetched >= MAX_DOCUMENTS) {
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

// Above the threshold the keyword is answered by the server; the loaded page
// only previews the first 100 documents.
async function searchServer() {
  const token = fence.begin()
  const kw = keyword.value.trim()
  if (!kw) {
    void load()
    return
  }
  loading.value = true
  error.value = ''
  try {
    const data = await overview(props.kb, 1, 'active', kw, 50)
    if (!fence.current(token)) return
    items.value = data.documents.filter(row => row.document_id !== props.exclude)
    total.value = data.total
    truncated.value = data.total > data.documents.length
  } catch {
    if (fence.current(token)) error.value = '无法搜索生效文档。'
  } finally {
    if (fence.current(token)) loading.value = false
  }
}
watch(keyword, () => {
  if (!serverMode.value) return
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { void searchServer() }, 350)
})
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
  return [{ label: t('workbench.picker.selectDocument'), value: '' }, ...rows]
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

onBeforeUnmount(() => { clearTimeout(searchTimer); fence.invalidate() })
</script>