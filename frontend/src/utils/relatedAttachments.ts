export interface RelatedAttachment {
  knowledge_id: string
  knowledge_base_id: string
  parent_document_id: string
  file_name: string
  download_url: string
  /** Deterministic semantic fields are optional; native download fields stay required. */
  display_name?: string
  original_filename?: string
  attachment_sequence?: number
  reference_title?: string
  reference_location?: string
  reference_excerpt?: string
  mapping_status?: 'confirmed' | 'candidate' | 'unmatched' | string
  mapping_confidence?: number
  parent_title?: string
  index_policy?: string
}

/** Collect structured server metadata; answer prose cannot create a download card. */
export function relatedAttachments(session: any): RelatedAttachment[] {
  const rows = [...(session?.knowledge_references || [])]
  for (const event of session?.agentEventStream || []) {
    rows.push(...(event?.tool_data?.results || []))
    if (event?.tool_data?.structured_content) rows.push(event.tool_data.structured_content)
  }
  const result: RelatedAttachment[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    let entries = row?.related_attachments ?? row?.metadata?.related_attachments
    if (typeof entries === 'string') { try { entries = JSON.parse(entries) } catch { continue } }
    if (!Array.isArray(entries)) continue
    for (const item of entries) {
      if (!item || !['knowledge_id', 'knowledge_base_id', 'parent_document_id', 'file_name', 'download_url'].every(key => typeof item[key] === 'string' && item[key])) continue
      if (!/^[a-zA-Z0-9_-]+$/.test(item.knowledge_id) || item.download_url !== `/api/v1/knowledge/${encodeURIComponent(item.knowledge_id)}/download`) continue
      const key = `${item.knowledge_base_id}:${item.knowledge_id}`
      if (!seen.has(key)) { result.push(item); seen.add(key) }
    }
  }
  const parentOrder = new Map<string, number>()
  for (const item of result) {
    if (!parentOrder.has(item.parent_document_id)) parentOrder.set(item.parent_document_id, parentOrder.size)
  }
  return result.sort((left, right) => {
    const leftParent = parentOrder.get(left.parent_document_id) ?? 0
    const rightParent = parentOrder.get(right.parent_document_id) ?? 0
    if (leftParent !== rightParent) return leftParent - rightParent
    const leftSequence = Number(left.attachment_sequence) > 0 ? Number(left.attachment_sequence) : Number.MAX_SAFE_INTEGER
    const rightSequence = Number(right.attachment_sequence) > 0 ? Number(right.attachment_sequence) : Number.MAX_SAFE_INTEGER
    if (leftSequence !== rightSequence) return leftSequence - rightSequence
    return (left.original_filename || left.file_name).localeCompare(right.original_filename || right.file_name)
  })
}

/** Keep only structured attachments named in the answer or linked from it. */
export function answerAttachments(session: any, answer: string): RelatedAttachment[] {
  const text = typeof answer === 'string' ? answer.trim() : ''
  if (!text) return []
  return relatedAttachments(session).filter(item => isAttachmentMentioned(item, text))
}

function isAttachmentMentioned(item: RelatedAttachment, text: string): boolean {
  const names = [item.display_name, item.original_filename, item.file_name]
    .map(value => (typeof value === 'string' ? value.trim() : ''))
    .filter(Boolean)
  for (const name of names) {
    if (containsToken(text, name)) return true
    const stem = name.replace(/\.[^.]+$/, '')
    if (stem && stem !== name && stem.length >= 3 && containsToken(text, stem)) return true
  }
  return typeof item.download_url === 'string' && item.download_url.length > 0 && text.includes(item.download_url)
}

function containsToken(text: string, token: string): boolean {
  const escaped = token.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`(^|[^A-Za-z0-9])${escaped}($|[^A-Za-z0-9])`, 'i').test(text)
}

/** Recognize native download anchors backed by this answer's metadata. */
export function attachmentAtTarget(target: any, session: any): RelatedAttachment | undefined {
  const href = target?.closest?.('a[href]')?.getAttribute('href')
  return relatedAttachments(session).find(item => item.download_url === href)
}
