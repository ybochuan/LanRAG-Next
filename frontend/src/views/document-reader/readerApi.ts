import { batchQueryKnowledge, getKnowledgeDetails } from '@/api/knowledge-base'
import { correctionDocument } from '@/api/business'
import { get } from '@/utils/request'
import { httpStatus } from '../business/model'
import type { ReaderDocument } from './model'

export interface NativeReaderDocument {
  id: string
  knowledge_base_id: string
  title?: string
  file_name?: string
  file_type?: string
  file_hash?: string
  file_path?: string
  updated_at?: string
  type?: string
}

type ReaderErrorCode = 'missing' | 'changed' | 'unsupported' | 'unverified' | 'stale'

export class ReaderError extends Error {
  constructor(readonly code: ReaderErrorCode) { super(code) }
}

export async function readerMetadata(doc: ReaderDocument): Promise<NativeReaderDocument> {
  const response = await getKnowledgeDetails(encodeURIComponent(doc.id)) as { success?: boolean; data?: NativeReaderDocument }
  if (!response.success || response.data?.id !== doc.id || response.data.knowledge_base_id !== doc.kb) throw new ReaderError('missing')
  return response.data
}

export async function readCurrentFile(doc: ReaderDocument, signal: AbortSignal) {
  signal.throwIfAborted()
  // The native preview resolves the current file by document ID. An observed
  // version ID carries no digest or resource binding for that response.
  if (doc.version || doc.kind) throw new ReaderError('unverified')
  const metadata = await readerMetadata(doc)
  signal.throwIfAborted()
  if (!metadata.file_name && metadata.type !== 'file' && metadata.type !== 'manual') throw new ReaderError('unsupported')
  const blob: unknown = await get(`/api/v1/knowledge/${encodeURIComponent(doc.id)}/preview`, { responseType: 'blob', signal })
  signal.throwIfAborted()
  if (!(blob instanceof Blob)) throw new ReaderError('missing')
  const after = await readerMetadata(doc)
  signal.throwIfAborted()
  if (!sameReaderFile(metadata, after)) throw new ReaderError('changed')
  return { metadata: after.type === 'manual' ? { ...after, file_type: 'md' } : after, blob }
}

export function sameReaderFile(before: NativeReaderDocument, after: NativeReaderDocument): boolean {
  return before.id === after.id && before.knowledge_base_id === after.knowledge_base_id && before.type === after.type &&
    before.file_hash === after.file_hash && before.file_path === after.file_path && before.file_name === after.file_name &&
    (before.type === 'manual' ? before.updated_at === after.updated_at : before.file_type === after.file_type)
}

export async function readCorrectionFile(doc: ReaderDocument, signal: AbortSignal) {
  if (!doc.version) throw new ReaderError('missing')
  await readerMetadata(doc)
  signal.throwIfAborted()
  const record = await correctionDocument(doc.kb, doc.id, doc.version, 'active')
  signal.throwIfAborted()
  if (record.knowledge_base_id !== doc.kb || record.document_id !== doc.id || record.version_id !== doc.version ||
    !record.items.length || record.items.some(item => item.state !== 'active')) throw new ReaderError('changed')
  return {
    metadata: { id: doc.id, knowledge_base_id: doc.kb, file_name: record.file_name, file_type: 'md' },
    blob: new Blob([record.markdown], { type: 'text/markdown' }),
  }
}

export async function readerTitles(kb: string, ids: string[]) {
  const unique = [...new Set(ids.filter(Boolean))]
  const titles = new Map<string, string>()
  for (let index = 0; index < unique.length; index += 50) {
    const query = new URLSearchParams()
    const batch = new Set(unique.slice(index, index + 50))
    for (const id of batch) query.append('ids', id)
    const response = await batchQueryKnowledge(query.toString(), kb) as { success?: boolean; data?: NativeReaderDocument[] }
    if (!response.success || !Array.isArray(response.data)) throw new ReaderError('missing')
    for (const doc of response.data) {
      if (doc.knowledge_base_id === kb && batch.has(doc.id)) titles.set(doc.id, doc.title || doc.file_name || doc.id)
    }
  }
  return titles
}

export function readerError(error: unknown): ReaderErrorCode | 'forbidden' | 'conflict' | 'unavailable' {
  if (error instanceof ReaderError) return error.code
  const status = httpStatus(error)
  if (status === 401 || status === 403) return 'forbidden'
  if (status === 404) return 'missing'
  if (status === 409) return 'conflict'
  return 'unavailable'
}
