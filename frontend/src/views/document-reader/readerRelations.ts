import type { RelatedDocument } from '@/api/business'
import { readerID, type ReaderDocument } from './model'
import { httpStatus } from '../business/model'

export interface VerifiedRelation {
  key: string
  row: RelatedDocument
  title: string
}

export function relationIdentity(row: RelatedDocument): string {
  return JSON.stringify([row.knowledge_base_id, row.document_id, row.relation_id ?? '', row.edge_type, row.direction])
}

export function sameKBRelations(source: ReaderDocument, rows: RelatedDocument[]): RelatedDocument[] {
  if (!Array.isArray(rows)) throw new Error('invalid_reader_relations')
  const unique = new Map<string, RelatedDocument>()
  for (const row of rows) {
    if (row?.knowledge_base_id !== source.kb || !readerID(row.document_id) || row.document_id === source.id ||
      (row.direction !== 'incoming' && row.direction !== 'outgoing') || typeof row.edge_type !== 'string' || !row.edge_type ||
      (row.relation_id !== undefined && typeof row.relation_id !== 'string')) continue
    unique.set(relationIdentity(row), row)
  }
  return [...unique.values()]
}

export async function verifyRelationTargets(
  kb: string,
  rows: RelatedDocument[],
  readTitles: (kb: string, ids: string[]) => Promise<Map<string, string>>,
  signal: AbortSignal,
): Promise<{ items: VerifiedRelation[]; status: 'ready' | 'empty' | 'partial' }> {
  const ids = [...new Set(rows.map(row => row.document_id))]
  const titles = new Map<string, string>()
  let failure: { error: unknown } | undefined
  for (let offset = 0; offset < ids.length; offset += 50) {
    signal.throwIfAborted()
    const batch = ids.slice(offset, offset + 50)
    try {
      const allowed = await readTitles(kb, batch)
      for (const id of batch) {
        const title = allowed.get(id)
        if (title) titles.set(id, title)
      }
    } catch (error) {
      const status = httpStatus(error)
      if (status === 401 || status === 403) throw error
      failure = { error }
    }
  }
  signal.throwIfAborted()
  const items = rows.flatMap(row => {
    const title = titles.get(row.document_id)
    return title ? [{ key: relationIdentity(row), row, title }] : []
  })
  if (failure && !items.length) throw failure.error
  return { items, status: failure ? 'partial' : items.length ? 'ready' : 'empty' }
}
