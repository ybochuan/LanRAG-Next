export interface RelatedAttachment {
  knowledge_id: string
  knowledge_base_id: string
  parent_document_id: string
  file_name: string
  download_url: string
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
  return result
}

/** Recognize native download anchors backed by this answer's metadata. */
export function attachmentAtTarget(target: any, session: any): RelatedAttachment | undefined {
  const href = target?.closest?.('a[href]')?.getAttribute('href')
  return relatedAttachments(session).find(item => item.download_url === href)
}
