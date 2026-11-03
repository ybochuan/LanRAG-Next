<template>
  <section v-if="items.length" class="related-attachments" aria-label="相关附件">
    <div class="related-attachments__title">相关附件</div>
    <t-card v-for="item in items" :key="item.knowledge_id" size="small" :bordered="true">
      <div class="related-attachments__row">
        <t-icon name="file-attachment" />
        <t-link :href="item.download_url" :disabled="!!loading[item.knowledge_id]" @click.prevent="download(item)">{{ item.file_name }}</t-link>
        <t-button size="small" variant="text" :loading="!!loading[item.knowledge_id]" @click="download(item)">下载</t-button>
      </div>
    </t-card>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { relatedAttachments, type RelatedAttachment } from '@/utils/relatedAttachments'
import { downloadRelatedAttachment } from '@/api/relatedAttachments'
const props = defineProps<{ session: any }>()
const items = computed(() => relatedAttachments(props.session))
const loading = reactive<Record<string, boolean>>({})
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
.related-attachments__title { font-size: 13px; color: var(--td-text-color-secondary); }
.related-attachments__row { display: flex; align-items: center; gap: 10px; }
.related-attachments__row .t-link { flex: 1; min-width: 0; overflow-wrap: anywhere; }
</style>
