import { get, put } from '@/utils/request'
import { batchQueryKnowledge } from '@/api/knowledge-base'
import { cached, cacheKey, invalidateCache } from './requestCache'

export type BusinessKind = 'relation' | 'feedback' | 'guidance' | 'revision'
export type BusinessSection = 'associations' | 'feedback-governance' | 'revisions'
export type BusinessAction = 'relation_declare' | 'relation_confirm' | 'relation_revoke'
  | 'feedback_create' | 'feedback_approve' | 'feedback_reject' | 'feedback_quarantine'
  | 'feedback_activate' | 'feedback_withdraw' | 'guidance_seed' | 'guidance_quarantine_topic'
  | 'revision_create' | 'revision_approve' | 'revision_reject' | 'revision_publish'
export interface BusinessPayload {
  document_id?: string; target_document_id?: string; edge_type?: string
  text?: string; topic?: string; risk?: string; negative?: boolean
  second_confirmation?: boolean; guidance_ids?: string[]
  similarity?: number; declaration?: number; structure?: number
}
export interface BusinessRecord {
  id: string; revision: number; status: string; text?: string; topic?: string
  risk?: string; ready_at?: string | null; confidence?: number; source?: string
  document_id?: string; target_document_id?: string; edge_type?: string
  source_document_id?: string; source_version_id?: string; target_version_id?: string
  proposed_patch?: string; guidance_ids?: string[]; publication_key?: string
  publish_error?: string; family_id?: string
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
