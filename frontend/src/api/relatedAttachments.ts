import { downKnowledgeDetails } from '@/api/knowledge-base'
import type { RelatedAttachment } from '@/utils/relatedAttachments'

/** Use native authenticated downloads; credentials never enter an href. */
export async function downloadRelatedAttachment(item: RelatedAttachment): Promise<void> {
  const blob = await downKnowledgeDetails(item.knowledge_id)
  if (!(blob instanceof Blob) || blob.type.includes('application/json')) throw new Error('附件当前不可下载')
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = item.file_name
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
