import { get, put } from '@/utils/request'
import { batchQueryKnowledge, getKnowledgeDetailsCon } from '@/api/knowledge-base'
import { cached, cacheKey, invalidateCache } from './requestCache'

export type BusinessKind = 'relation' | 'feedback' | 'guidance' | 'revision'
export type BusinessSection = 'associations' | 'feedback-governance' | 'revisions'
export type BusinessAction = 'relation_declare' | 'relation_confirm' | 'relation_revoke'
  | 'repeal_create' | 'repeal_approve' | 'repeal_cancel'
  | 'clause_create' | 'clause_confirm' | 'clause_resolve' | 'clause_revoke'
  | 'feedback_create' | 'feedback_approve' | 'feedback_reject' | 'feedback_quarantine'
  | 'feedback_activate' | 'feedback_withdraw' | 'guidance_seed' | 'guidance_quarantine_topic'
  | 'revision_create' | 'revision_approve' | 'revision_reject' | 'revision_publish'
  | 'split_create' | 'split_approve' | 'split_reject'
export interface SplitPartInput { title: string; text: string }
export interface ClauseAnchor { chunk_id: string; content_hash: string }
export interface ClauseChunk {
  id: string; knowledge_id: string; knowledge_base_id: string
  content: string; chunk_index: number
}
export interface SplitPartRecord {
  id: string; title: string; proposed_patch: string; revision: number; status: string
  target_document_id?: string; publish_error?: string
}
export interface BusinessPayload {
  document_id?: string; target_document_id?: string; edge_type?: string
  text?: string; topic?: string; risk?: string; negative?: boolean
  second_confirmation?: boolean; guidance_ids?: string[]
  similarity?: number; declaration?: number; structure?: number
  split_parts?: SplitPartInput[]
  effective_at?: string
  source_clause?: ClauseAnchor; target_clause?: ClauseAnchor
  applies_when?: string; resolution?: string
}
export interface BusinessRecord {
  id: string; revision: number; status: string; text?: string; topic?: string
  risk?: string; ready_at?: string | null; confidence?: number; source?: string
  document_id?: string; target_document_id?: string; edge_type?: string
  source_document_id?: string; source_version_id?: string; target_version_id?: string
  effective_at?: string; applied_at?: string | null
  source_clause?: ClauseAnchor; target_clause?: ClauseAnchor
  applies_when?: string; resolution?: string; decision_note?: string
  resolved_by?: string; resolved_at?: string | null; pins_current?: boolean
  proposed_patch?: string; guidance_ids?: string[]; publication_key?: string
  publish_error?: string; family_id?: string
  record_kind?: 'split_batch' | 'split_part'
  split_id?: string; split_title?: string; split_parts?: SplitPartRecord[]
}
export interface BusinessResult { id: string; revision: number; status: string; target_document_id?: string }
export interface BusinessBody { operation_id: string; expected_revision: number; business: { id?: string; payload: BusinessPayload } }
export interface RelatedDocument { document_id: string; edge_type: string; direction: string; confidence: number }
interface Envelope<T> { success: boolean; data: T; error?: { code?: string } }
const base = (kb: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle/business/`
function cacheTenant(){try{return localStorage.getItem('weknora_selected_tenant_id')||localStorage.getItem('weknora_tenant')||''}catch{return ''}}
async function unwrap<T>(request: Promise<unknown>): Promise<T> {
  const response = await request as Envelope<T>
  if (!response.success || response.data === null || response.data === undefined) throw new Error(response.error?.code || 'business_unavailable')
  return response.data
}
export const list = (kb: string, kind: BusinessKind, page = 1, id = '') => unwrap<BusinessRecord[]>(get(`${base(kb)}${kind}_list?page=${page}&page_size=20&id=${encodeURIComponent(id)}`))
export const write = async (kb: string, action: BusinessAction, body: BusinessBody) => {
  const result = await unwrap<BusinessResult>(put(`${base(kb)}${action}`, body))
  invalidateCache('document-picker-active')
  invalidateCache('doc-titles')
  return result
}
export interface AnswerFeedbackPayload {
  session_id: string
  message_id: string
  topic: string
  text: string
  negative: boolean
}
export const captureAnswerFeedback = (kb: string, operationId: string, payload: AnswerFeedbackPayload) =>
  unwrap<BusinessResult>(put(`${base(kb)}feedback_capture`, {
    operation_id: operationId,
    expected_revision: 0,
    business: { payload },
  }))
export const related = (kb: string, document: string) => unwrap<RelatedDocument[]>(get(`${base(kb)}related?document_id=${encodeURIComponent(document)}`))

export async function clauseChunks(kb: string, document: string, page: number) {
  const response = await getKnowledgeDetailsCon(encodeURIComponent(document), page) as Envelope<ClauseChunk[]> & { total: number; page_size: number }
  if (!response?.success || response.page_size < 1 || !Number.isFinite(response.total)) throw new Error('clause_chunks_unavailable')
  return {
    rows: (response.data || []).filter(row => row.knowledge_base_id === kb && row.knowledge_id === document && typeof row.content === 'string' && row.content.trim()),
    hasNext: page * response.page_size < response.total,
  }
}

export async function clauseAnchor(chunk: ClauseChunk): Promise<ClauseAnchor> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(chunk.content))
  return { chunk_id: chunk.id, content_hash: Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('') }
}

export async function clauseEvidence(kb: string, document: string, expected: ClauseAnchor) {
  const chunk = await unwrap<ClauseChunk>(get(`/api/v1/chunks/by-id/${encodeURIComponent(expected.chunk_id)}`))
  if (chunk.id !== expected.chunk_id || chunk.knowledge_base_id !== kb || chunk.knowledge_id !== document || typeof chunk.content !== 'string') return
  if ((await clauseAnchor(chunk)).content_hash === expected.content_hash) return chunk.content
}

export async function documentTitle(kb: string, id: string): Promise<string> {
  const value = await unwrap<{ title: string; knowledge_base_id: string }>(get(`/api/v1/knowledge/${encodeURIComponent(id)}`))
  return value.knowledge_base_id === kb ? value.title : id
}

/**
 * Resolves a set of document titles in one native batch request instead of
 * issuing one GET /api/v1/knowledge/:id per document.
 */
export async function documentTitles(kb: string, ids: string[]): Promise<Record<string, string>> {
  const unique = [...new Set(ids.filter((id): id is string => typeof id === 'string' && !!id))].sort()
  if (!unique.length) return {}
  return cached(cacheKey('doc-titles', cacheTenant(), kb, unique), async () => {
    const query = new URLSearchParams()
    for (const id of unique) query.append('ids', id)
    const response = await batchQueryKnowledge(query.toString(), kb) as {
      success?: boolean
      data?: Array<{ id: string; title: string; knowledge_base_id: string }>
    }
    const result: Record<string, string> = {}
    if (!response?.success || !Array.isArray(response.data)) return result
    for (const row of response.data) {
      if (row && row.knowledge_base_id === kb && typeof row.title === 'string') result[row.id] = row.title
    }
    return result
  }, 60_000)
}
