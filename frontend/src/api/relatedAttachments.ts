import { downKnowledgeDetails } from '@/api/knowledge-base'
import type { RelatedAttachment } from '@/utils/relatedAttachments'
import { downloadResponseName } from '@/utils/request'
import { saveBusinessDownload } from '@/utils/desktopDownload'

/** Use native authenticated downloads; credentials never enter an href. */
export async function downloadRelatedAttachment(item: RelatedAttachment): Promise<void> {
  const blob = await downKnowledgeDetails(item.knowledge_id)
  if (!(blob instanceof Blob) || blob.type.includes('application/json')) throw new Error('附件当前不可下载')
  const fileName = downloadResponseName(blob, item.file_name)
  await saveBusinessDownload(blob, {
    fileName,
    sourceType: 'attachment',
    sourceId: item.knowledge_id,
    sourceTitle: item.display_name || item.original_filename || item.file_name,
    link: `/platform/knowledge-bases/${encodeURIComponent(item.knowledge_base_id)}?knowledge_id=${encodeURIComponent(item.knowledge_id)}`,
  })
}
