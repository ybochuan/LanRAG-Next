<template>
  <div class="reading-relation-preview">
    <p v-if="loading" role="status">{{ $t('workbench.businessUi.reading.checking') }}</p>
    <t-alert v-else-if="error" theme="warning" role="alert">{{ error }}</t-alert>
    <DocumentPreview v-else-if="document" :knowledge-id="documentId" :file-type="document.fileType"
      :file-name="document.title" active disable-auto-focus />
  </div>
</template>

<script setup lang="ts">
import { defineAsyncComponent, ref, watch } from 'vue'
import { related } from '@/api/business'
import { getKnowledgeDetails } from '@/api/knowledge-base'
import { useI18n } from 'vue-i18n'

const DocumentPreview = defineAsyncComponent(() => import('@/components/document-preview.vue'))
const props = defineProps<{ kb: string; parentId: string; documentId: string; edgeType: string; relationId?: string }>()
const { t } = useI18n()
const document = ref<{ title: string; fileType: string }>()
const loading = ref(false)
const error = ref('')

watch(() => [
  props.kb, props.parentId, props.documentId, props.edgeType, props.relationId,
] as const, async ([kb, parent, id, type, relationId], _previous, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  document.value = undefined
  error.value = ''
  loading.value = true
  try {
    const rows = await related(kb, parent)
    if (cancelled) return
    if (!rows.some(row => row.knowledge_base_id === kb && row.document_id === id && row.direction === 'incoming' &&
      row.edge_type === type && (!relationId || row.relation_id === relationId))) {
      error.value = t('workbench.businessUi.reading.inactive')
      return
    }
    const response = await getKnowledgeDetails(encodeURIComponent(id))
    if (cancelled) return
    if (!response.success || response.data?.knowledge_base_id !== kb) {
      error.value = t('workbench.businessUi.reading.unreadable')
      return
    }
    const { title, file_name, file_type } = response.data
    document.value = { title: title || file_name || id, fileType: file_type || '' }
  } catch {
    if (!cancelled) error.value = t('workbench.businessUi.reading.failed')
  } finally {
    if (!cancelled) loading.value = false
  }
}, { immediate: true })
</script>

<style scoped>
.reading-relation-preview {
  min-width: 0;
  margin-top: 8px;
  padding: 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-medium);
}
</style>
