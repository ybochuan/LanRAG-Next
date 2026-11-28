import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { sha256 } from 'js-sha256'
import type { SourceLocateRequest } from '@/utils/sourceLocator'

export interface ReaderDocument {
  kb: string
  id: string
  version?: string
  kind?: 'correction'
}

export interface ReaderIntent {
  left?: ReaderDocument
  right?: ReaderDocument
  view?: 'history' | 'sources'
}

export function readerID(value: unknown): string | undefined {
  return typeof value === 'string' && /^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$/.test(value) ? value : undefined
}

export function readerIntent(query: LocationQuery): ReaderIntent | undefined {
  const kb = readerID(query.kb)
  const id = readerID(query.doc)
  const compare = readerID(query.compare)
  const compareKB = readerID(query.compareKB ?? query.kb)
  const version = readerID(query.version)
  const compareVersion = readerID(query.compareVersion)
  const correction = query.compareKind === 'correction'
  const view = query.view === 'history' || query.view === 'sources' ? query.view : undefined
  if (query.view !== undefined && !view) return
  if ((query.doc !== undefined || query.version !== undefined || query.view !== undefined) && (!kb || !id)) return
  if ((query.version !== undefined && !version) || (query.compareVersion !== undefined && !compareVersion)) return
  if ((query.compare !== undefined || query.compareKB !== undefined || query.compareVersion !== undefined || query.compareKind !== undefined) && (!kb || !id || !compare || !compareKB)) return
  if (query.compareKind !== undefined && (!correction || !version || compareVersion !== version || compare !== id || compareKB !== kb)) return
  return {
    left: kb && id ? { kb, id, version } : undefined,
    right: compareKB && compare ? { kb: compareKB, id: compare, version: compareVersion, kind: correction ? 'correction' : undefined } : undefined,
    view,
  }
}

export function readerQuery(intent: ReaderIntent): LocationQueryRaw {
  const { left, right, view } = intent
  return { kb: left?.kb, doc: left?.id, version: left?.version, compare: right?.id,
    compareKB: right?.kb !== left?.kb ? right?.kb : undefined, compareVersion: right?.version, compareKind: right?.kind, view }
}

export function readerKey(doc: ReaderDocument): string {
  return JSON.stringify([doc.kb, doc.id, doc.version ?? '', doc.kind ?? ''])
}

export function readerRoute(left: ReaderDocument, right?: ReaderDocument, view?: ReaderIntent['view']) {
  return { name: 'document-reader', query: readerQuery({ left, right, view }) }
}

export type ReaderQuestion = { left: ReaderDocument; right?: ReaderDocument }

function questionDocument(value: unknown): ReaderDocument | undefined {
  if (!value || typeof value !== 'object') return
  const { kb, id, version, kind } = value as Partial<ReaderDocument>
  if (version !== undefined || kind !== undefined) return
  const validKB = readerID(kb)
  const validID = readerID(id)
  if (validKB && validID) return { kb: validKB, id: validID }
}

export function readerQuestion(left: unknown, right?: unknown): ReaderQuestion | undefined {
  const first = questionDocument(left)
  const second = questionDocument(right)
  if (!first || (right !== undefined && !second) || second?.id === first.id) return
  return second ? { left: first, right: second } : { left: first }
}

export function readReaderQuestion(value: unknown): ReaderQuestion | undefined {
  if (typeof value !== 'string' || value.length > 1024) return
  try {
    const parsed = JSON.parse(value)
    return readerQuestion(parsed?.left, parsed?.right)
  } catch { /* Ignore invalid navigation state. */ }
}

export function citationDestination(doc: ReaderDocument, scope?: ReaderQuestion): ReaderQuestion & { side: 'A' | 'B' } {
  if (scope && readerKey(scope.left) === readerKey(doc)) return { ...scope, side: 'A' }
  if (scope?.right && readerKey(scope.right) === readerKey(doc)) return { ...scope, side: 'B' }
  return { left: doc, side: 'A' }
}

export function citationLocate(request: SourceLocateRequest, expectedHash: string, currentHash?: string): SourceLocateRequest {
  return !request.unavailable && expectedHash && expectedHash === currentHash
    ? request
    : { token: request.token, locators: [], quotes: [], unavailable: true }
}

export function manualCitationFingerprint(content: string | ArrayBuffer): string {
  return `manual:${sha256(content)}`
}

export const relationGroups = ['references', 'companion', 'history', 'attachments', 'reading', 'other'] as const
export type RelationGroup = typeof relationGroups[number]

export function relationGroup(type: string): RelationGroup {
  switch (type) {
    case 'cites': return 'references'
    case 'supplements': case 'implements': case 'interprets': case 'amends': return 'companion'
    case 'repeals': case 'supersedes': case 'splits_into': case 'merges_into': return 'history'
    case 'attachment_of': case 'template_for': return 'attachments'
    case 'explains_more': case 'reading_note': case 'applies_exception_to': case 'conflicts_with': return 'reading'
    default: return 'other'
  }
}
