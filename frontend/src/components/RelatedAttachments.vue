<template>
  <section v-if="items.length" class="related-attachments" aria-label="相关附件">
    <div class="related-attachments__title">相关附件 · {{ items.length }}</div>
    <template v-for="group in groups" :key="group.id">
      <div v-if="group.label" class="related-attachments__subtitle">{{ group.label }}</div>
      <ul class="related-attachments__list">
        <li v-for="item in group.items" :key="item.knowledge_id" class="related-attachments__row">
          <t-icon name="file-attachment" class="related-attachments__icon" aria-hidden="true" />
          <div class="related-attachments__body">
            <div class="related-attachments__name" :title="item.original_filename || undefined">{{ displayName(item) }}</div>
            <div v-if="item.parent_title" class="related-attachments__meta">来源：{{ item.parent_title }}</div>
            <div class="related-attachments__detail">
              <span v-if="group.id === 'mapped' && referenceText(item)" class="related-attachments__meta">{{ referenceText(item) }}</span>
              <t-tag v-if="item.mapping_status === 'candidate'" size="small" variant="light" theme="warning">对应关系待确认</t-tag>
              <t-tag v-else-if="item.mapping_status === 'manual' || item.mapping_status === 'confirmed'"
                size="small" variant="light" theme="success">对应关系已确认</t-tag>
              <t-tag v-else-if="item.mapping_status === 'template'" size="small" variant="light">共享模板</t-tag>
              <span v-else class="related-attachments__meta">用途待识别</span>
            </div>
          </div>
          <t-button size="small" variant="outline" theme="primary" :aria-label="`下载 ${displayName(item)}`"
            :loading="!!loading[item.knowledge_id]" @click="download(item)">
            <template #icon><t-icon name="download" /></template>
            {{ downloadLabel(item) }}
          </t-button>
        </li>
      </ul>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { answerAttachments, type RelatedAttachment } from '@/utils/relatedAttachments'
import { downloadRelatedAttachment } from '@/api/relatedAttachments'
const props = defineProps<{ session: any; answer?: string }>()
const items = computed(() => answerAttachments(props.session, props.answer ?? props.session?.content ?? ''))
const mappedStates = new Set(['confirmed', 'candidate', 'manual', 'template'])
const groups = computed(() => [
  { id: 'mapped', label: '', items: items.value.filter(item => mappedStates.has(item.mapping_status || '')) },
  { id: 'other', label: '其他附件', items: items.value.filter(item => !mappedStates.has(item.mapping_status || '')) },
].filter(group => group.items.length))
const loading = reactive<Record<string, boolean>>({})
function displayName(item: RelatedAttachment) {
  return item.display_name?.trim() || item.file_name
}
function referenceText(item: RelatedAttachment) {
  const referenceTitle = item.reference_title?.trim()
  const location = item.reference_location?.trim()
  const title = referenceTitle === displayName(item) || referenceTitle === location ? '' : referenceTitle
  return [title, location].filter(Boolean).join(' · ')
}
function downloadLabel(item: RelatedAttachment) {
  const extension = (item.original_filename || item.file_name).match(/\.([a-z0-9]{2,8})$/i)?.[1]
  return extension ? `下载 ${extension.toUpperCase()}` : '下载'
}
async function download(item: RelatedAttachment) {
  if (loading[item.knowledge_id]) return
  loading[item.knowledge_id] = true
  try { await downloadRelatedAttachment(item) }
  catch { MessagePlugin.error('附件当前不可下载，所属制度可能已过期或访问权限已变化') }
  finally { loading[item.knowledge_id] = false }
}
</script>
<style scoped>
.related-attachments { display: grid; gap: 8px; margin-top: 12px; }
.related-attachments__title {
  color: var(--td-text-color-primary);
  font-size: var(--td-font-size-title-medium);
  font-weight: 600;
}
.related-attachments__subtitle, .related-attachments__meta {
  color: var(--td-text-color-secondary);
  font-size: var(--td-font-size-body-small);
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.related-attachments__list {
  min-width: 0;
  margin: 0;
  padding: 0;
  list-style: none;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-medium);
}
.related-attachments__row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  column-gap: 10px;
  padding: 10px 12px;
}
.related-attachments__row + .related-attachments__row { border-top: 1px solid var(--td-component-stroke); }
.related-attachments__icon { margin-top: 3px; color: var(--td-text-color-secondary); font-size: 16px; }
.related-attachments__body { min-width: 0; }
.related-attachments__name {
  color: var(--td-text-color-primary);
  font-size: var(--td-font-size-body-medium);
  font-weight: 500;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.related-attachments__detail { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; margin-top: 2px; }
.related-attachments__detail .t-tag { max-width: 100%; }
</style>
