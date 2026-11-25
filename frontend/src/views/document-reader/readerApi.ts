import { batchQueryKnowledge, getKnowledgeDetails } from '@/api/knowledge-base'
import { correctionDocument } from '@/api/business'
import { get } from '@/utils/request'
import { getPreviewMimeType, resolveFilePreviewExt } from '@/utils/filePreview'
import { httpStatus } from '../business/model'
import type { ReaderDocument } from './model'

export interface NativeReaderDocument {
  id: string
  knowledge_base_id: string
  title?: string
  file_name?: string
  file_type?: string
  file_size?: number
  file_hash?: string
  file_path?: string
  updated_at?: string
  type?: string
}

type ReaderErrorCode = 'missing' | 'changed' | 'unsupported' | 'unverified' | 'stale' | 'tooLarge'

export const READER_ZRIMO_MAX_BYTES = 50 * 1024 * 1024
/** Kept for callers that only know the former DOCX-specific boundary. */
export const READER_DOCX_MAX_BYTES = READER_ZRIMO_MAX_BYTES

const ZRIMO_FORMATS: Record<string, string> = {
  doc: 'doc', docx: 'docx', docm: 'docm',
  xls: 'xls', xlsx: 'xlsx', xlsm: 'xlsm',
  ppt: 'ppt', pptx: 'pptx', pptm: 'pptm', ppsx: 'ppsx',
  pdf: 'pdf', csv: 'csv', tsv: 'tsv',
  png: 'png', jpg: 'jpeg', jpeg: 'jpeg', gif: 'gif', webp: 'webp', svg: 'svg', bmp: 'bmp', tif: 'tiff', tiff: 'tiff',
}

const ZRIMO_MIME: Record<string, string> = {
  'application/pdf': 'pdf',
  'application/msword': 'doc',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'docx',
  'application/vnd.ms-excel': 'xls',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'xlsx',
  'application/vnd.ms-powerpoint': 'ppt',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'pptx',
  'text/csv': 'csv',
  'text/tab-separated-values': 'tsv',
  'image/png': 'png',
  'image/jpeg': 'jpeg',
  'image/gif': 'gif',
  'image/webp': 'webp',
  'image/svg+xml': 'svg',
  'image/bmp': 'bmp',
  'image/tiff': 'tiff',
}

function readerExtension(metadata: Pick<NativeReaderDocument, 'file_name' | 'file_type'>): string {
  for (const raw of [metadata.file_type, metadata.file_name]) {
    const value = String(raw || '').trim().toLowerCase().replace(/^\./, '')
    if (!value) continue
    const tail = value.includes('.') ? value.slice(value.lastIndexOf('.') + 1) : value
    if (ZRIMO_FORMATS[tail]) return tail
    if (ZRIMO_MIME[value]) return ZRIMO_MIME[value]
  }
  return ''
}

/** Return the canonical extension accepted by Zrimo, or undefined for a native-only format. */
export function zrimoFormat(metadata: Pick<NativeReaderDocument, 'file_name' | 'file_type'>): string | undefined {
  const ext = readerExtension(metadata)
  return ext ? ZRIMO_FORMATS[ext] : undefined
}

export function isZrimoReadable(metadata: NativeReaderDocument): boolean {
  return metadata.type !== 'manual' && !!zrimoFormat(metadata)
}

export function isReaderDocx(metadata: NativeReaderDocument): boolean {
  return metadata.type !== 'manual' && resolveFilePreviewExt(metadata.file_name, metadata.file_type) === 'docx'
}

export class ReaderError extends Error {
  constructor(readonly code: ReaderErrorCode) { super(code) }
}

export async function readerMetadata(doc: ReaderDocument): Promise<NativeReaderDocument> {
  const response = await getKnowledgeDetails(encodeURIComponent(doc.id)) as { success?: boolean; data?: NativeReaderDocument }
  if (!response.success || response.data?.id !== doc.id || response.data.knowledge_base_id !== doc.kb) throw new ReaderError('missing')
  return response.data
}

async function readZrimoPreview(url: string, signal: AbortSignal, format: string): Promise<Blob> {
  const response = await get<Blob | ReadableStream<Uint8Array<ArrayBuffer>>>(url, { adapter: 'fetch', responseType: 'stream', signal })
  if (response instanceof Blob) {
    if (response.size > READER_ZRIMO_MAX_BYTES) throw new ReaderError('tooLarge')
    return response
  }
  if (!(response instanceof ReadableStream)) throw new ReaderError('missing')
  const reader = response.getReader()
  const chunks: Uint8Array<ArrayBuffer>[] = []
  let size = 0
  try {
    for (;;) {
      signal.throwIfAborted()
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > READER_ZRIMO_MAX_BYTES) throw new ReaderError('tooLarge')
      chunks.push(value)
    }
  } finally {
    // Also cancels the remaining response on size limit, failure or pane closure.
    void reader.cancel().catch(() => {})
    reader.releaseLock()
  }
  return new Blob(chunks, { type: getPreviewMimeType(format === 'jpeg' ? 'jpg' : format === 'tiff' ? 'tif' : format) })
}

/** Compatibility wrapper for callers that explicitly request the DOCX path. */
export async function readDocxPreview(url: string, signal: AbortSignal): Promise<Blob> {
  return readZrimoPreview(url, signal, 'docx')
}

export async function readCurrentFile(doc: ReaderDocument, signal: AbortSignal) {
  signal.throwIfAborted()
  // The native preview resolves the current file by document ID. An observed
  // version ID carries no digest or resource binding for that response.
  if (doc.version || doc.kind) throw new ReaderError('unverified')
  const metadata = await readerMetadata(doc)
  signal.throwIfAborted()
  if (!metadata.file_name && metadata.type !== 'file' && metadata.type !== 'manual') throw new ReaderError('unsupported')
  const preview = `/api/v1/knowledge/${encodeURIComponent(doc.id)}/preview`
  const format = zrimoFormat(metadata)
  if (format && (metadata.file_size ?? 0) > READER_ZRIMO_MAX_BYTES) throw new ReaderError('tooLarge')
  const blob: unknown = format
    ? await readZrimoPreview(preview, signal, format)
    : await get(preview, { responseType: 'blob', signal })
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
    before.file_size === after.file_size &&
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
  if (status === 413) return 'tooLarge'
  return 'unavailable'
}
