import { get } from '@/utils/request'

export type FamilyTab = 'members' | 'removed' | 'relations' | 'history' | 'activity' | 'replacements' | 'unassigned' | 'suggestions'
export type WorkspaceTab = Exclude<FamilyTab, 'replacements' | 'unassigned' | 'suggestions'>

export interface BusinessFamily {
  business_family_id: string
  label: string
  revision: number
  published_document_id?: string
  published_version_node_id?: string
}
export interface FamilyMember {
  document_id: string
  state?: string
  removed_at?: string
  reason?: string
}
export interface FamilyRelation {
  id: string
  document_id: string
  target_document_id: string
  edge_type: string
  status: string
  revision: number
  scope: 'internal' | 'external'
  current: boolean
}
export interface FamilyActivity {
  operation_id: string
  actor_id: string
  action: string
  aggregate_id: string
  status: string
  occurred_at: string
}
export interface FamilyReplacement {
  id: string
  business_family_id: string
  family_revision: number
  old_document_id: string
  old_version_node_id: string
  new_document_id: string
  effective_at: string
  applied_at?: string
  revision: number
  status: string
}
export interface FamilyReplacementHistory {
  business_family_id: string
  family_revision: number
  published_document_id?: string
  final_document_id?: string
  steps: FamilyReplacement[]
  fingerprint?: string
  blocked_reason?: string
}
export interface FamilyWorkspace {
  family: BusinessFamily
  view: WorkspaceTab
  has_next: boolean
  can_write: boolean
  members?: FamilyMember[]
  relations?: FamilyRelation[]
  activity?: FamilyActivity[]
}
export interface SuggestionPolicy {
  revision: number
  similarity_weight: number
  declaration_weight: number
  structure_weight: number
  threshold: number
}
export interface FamilySuggestion {
  id: string
  revision: number
  kind: 'membership' | 'relationship'
  document_id: string
  target_document_id: string
  edge_type?: string
  confidence: number
  evidence: {
    similarity: number
    title_similarity?: number
    semantic_similarity?: number
    semantic_model?: string
    declaration: number
    structure: number
    reasons: string[]
  }
}
export interface FamilySuggestionInbox {
  policy: SuggestionPolicy
  items: FamilySuggestion[]
  has_next: boolean
  scan_status: 'idle' | 'queued' | 'completed' | 'failed'
}

async function read<T>(kb: string, action: string, id: string, page: number, state = '') {
  const query = new URLSearchParams({ id, page: String(page), page_size: '20', state })
  const response = await get(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle/business/${action}?${query}`) as {
    success: boolean; data: T; error?: { code: string }
  }
  if (!response.success || response.data == null) throw new Error(response.error?.code || 'family_unavailable')
  return response.data
}

export const listFamilies = (kb: string, page: number) => read<BusinessFamily[]>(kb, 'family_list', '', page)
export const unassignedMembers = (kb: string, page: number) => read<FamilyMember[]>(kb, 'family_members', '', page)
export const replacements = (kb: string, id: string, page: number) => read<FamilyReplacement[]>(kb, 'family_replacements', id, page, 'timeline')
export const replacementHistory = (kb: string, id: string) => read<FamilyReplacementHistory>(kb, 'family_replacements', id, 1, 'history-preview')
export const workspace = (kb: string, id: string, view: WorkspaceTab, page: number) => read<FamilyWorkspace>(kb, 'family_workspace', id, page, view)
export const suggestions = (kb: string, id: string, page: number, documentId = '') => read<FamilySuggestionInbox>(kb, 'family_suggestions', id, page, documentId)
