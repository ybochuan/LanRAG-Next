<template>
  <section v-if="items.length" class="related-attachments" aria-label="相关附件">
    <div class="related-attachments__title">
      <span>相关附件（{{ items.length }}）</span>
      <span class="related-attachments__hint">可下载</span>
    </div>
    <t-card v-for="item in mappedItems" :key="item.knowledge_id" size="small" :bordered="true">
      <div class="related-attachments__row">
        <t-icon name="file-attachment" />
        <div class="related-attachments__body">
          <t-link :href="item.download_url" :disabled="!!loading[item.knowledge_id]" @click.prevent="download(item)">{{ displayName(item) }}</t-link>
          <div v-if="item.original_filename" class="related-attachments__meta">原文件：{{ item.original_filename }}</div>
          <div v-if="referenceText(item)" class="related-attachments__meta">对应正文：{{ referenceText(item) }}</div>
          <div v-if="item.parent_title" class="related-attachments__meta">来源：{{ item.parent_title }}</div>
          <t-tag v-if="item.mapping_status === 'candidate'" size="small" variant="light" theme="warning">待确认</t-tag>
        </div>
        <t-button size="small" variant="text" :loading="!!loading[item.knowledge_id]" @click="download(item)">下载</t-button>
      </div>
    </t-card>
    <template v-if="otherItems.length">
      <div class="related-attachments__subtitle">其他附件</div>
      <t-card v-for="item in otherItems" :key="item.knowledge_id" size="small" :bordered="true">
        <div class="related-attachments__row">
          <t-icon name="file-attachment" />
          <div class="related-attachments__body">
            <t-link :href="item.download_url" :disabled="!!loading[item.knowledge_id]" @click.prevent="download(item)">{{ item.file_name }}</t-link>
            <div v-if="item.original_filename" class="related-attachments__meta">原文件：{{ item.original_filename }}</div>
            <div class="related-attachments__meta">具体用途待识别</div>
          </div>
          <t-button size="small" variant="text" :loading="!!loading[item.knowledge_id]" @click="download(item)">下载</t-button>
        </div>
      </t-card>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { relatedAttachments, type RelatedAttachment } from '@/utils/relatedAttachments'
import { downloadRelatedAttachment } from '@/api/relatedAttachments'
const props = defineProps<{ session: any }>()
const items = computed(() => relatedAttachments(props.session))
const mappedItems = computed(() => items.value.filter(item => item.mapping_status === 'confirmed' || item.mapping_status === 'candidate'))
const otherItems = computed(() => items.value.filter(item => item.mapping_status !== 'confirmed' && item.mapping_status !== 'candidate'))
const loading = reactive<Record<string, boolean>>({})
function displayName(item: RelatedAttachment) {
  return item.display_name?.trim() || item.file_name
}
function referenceText(item: RelatedAttachment) {
  const title = item.reference_title?.trim() || ''
  const location = item.reference_location?.trim() || ''
  if (title && location && title !== location) return `${title}（${location}）`
  return title || location
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
.related-attachments__title { display: flex; align-items: center; gap: 8px; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.related-attachments__hint { font-size: var(--app-text-xs); color: var(--td-text-color-placeholder); }
.related-attachments__subtitle { margin-top: 4px; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.related-attachments__row { display: flex; align-items: flex-start; gap: 10px; }
.related-attachments__body { display: grid; flex: 1; min-width: 0; gap: 3px; }
.related-attachments__body .t-link { overflow-wrap: anywhere; }
.related-attachments__meta { font-size: var(--app-text-xs); line-height: 1.4; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.related-attachments__row .t-button { flex: none; }
</style>
