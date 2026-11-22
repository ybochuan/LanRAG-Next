import { get, put } from '@/utils/request'
import { invalidateCache } from './requestCache'

interface Envelope<T> { success: boolean; data: T; error?: { code?: string } }

export interface AttachmentReference {
  title?: string
  location?: string
  excerpt?: string
  sequence?: number
}

export interface AttachmentItem {
  id: string
  file_hash: string
  title: string
  native_title: string
  file_name: string
  folder_path: string
  mapping_status: string
  parse_status: string
  state: string
  title_state?: string
  reclaim_error?: string
  resources_verified?: boolean
  revision: number
  relation_id?: string
  relation_type: string
  managed: boolean
  reference: AttachmentReference
}

export interface AttachmentPreview {
  fingerprint: string
  file_name: string
  display_name: string
  mapping_status: string
  state: string
}

export interface AttachmentImportOutcome {
  fingerprint: string
  file_name: string
  document_id?: string
  error?: string
}

export interface AttachmentExtractionIssue {
  file_name: string
  reason: string
}

export interface AttachmentTaskItem {
  id: string
  title: string
  native_title: string
  file_name: string
  display_name: string
  mapping_status: string
  reference_title: string
  reference_location: string
  title_state: string
  reasons: string[]
}

export interface AttachmentTaskDocument {
  parent_id: string
  parent_title: string
  parent_file_name: string
  count: number
  items: AttachmentTaskItem[]
}

export interface AttachmentTaskList {
  documents: AttachmentTaskDocument[]
  total: number
  truncated: boolean
}

export interface AttachmentWorkbench {
  parent_id: string
  parent_version: string
  parent_file_name: string
  items: AttachmentItem[]
  references?: (AttachmentReference & { id: string })[]
  preview?: AttachmentPreview[]
  preview_issues?: AttachmentExtractionIssue[]
}

type Action = 'confirm' | 'detach' | 'restore' | 'import'
const base = (kb: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle/business/attachment_`
const query = (doc: string) => new URLSearchParams({ document_id: doc })

async function unwrap<T>(request: Promise<unknown>): Promise<T> {
  const response = await request as Envelope<T>
  if (!response?.success || response.data == null) throw new Error(response?.error?.code || 'attachment_management_unavailable')
  return response.data
}

export function attachmentTasks(kb: string) {
  return unwrap<AttachmentTaskList>(get(`${base(kb)}tasks`, { timeout: 50_000 }))
}

export function attachmentWorkbench(kb: string, doc: string, preview = false) {
  return unwrap<AttachmentWorkbench>(get(`${base(kb)}${preview ? 'preview' : 'list'}?${query(doc)}`, { timeout: 50_000 }))
}

export async function updateAttachment(kb: string, doc: string, action: Action, revision: number, business: object, operationID: string) {
  const result = await unwrap<{ title_state?: string; items?: AttachmentImportOutcome[] }>(put(`${base(kb)}${action}?${query(doc)}`, {
    expected_revision: revision,
    operation_id: operationID,
    business,
  }, { timeout: 50_000 }))
  invalidateCache('doc-titles')
  invalidateCache('document-picker-active')
  return result
}

export function uploadWorkbenchAttachment(kb: string, doc: string, version: string, file: File) {
  const params = query(doc)
  params.set('parent_version', version)
  params.set('file_name', file.name)
  return unwrap<{ document_id: string }>(put(`${base(kb)}upload?${params}`, file, {
    timeout: 50_000,
    headers: { 'Content-Type': 'application/octet-stream' },
  }))
}
