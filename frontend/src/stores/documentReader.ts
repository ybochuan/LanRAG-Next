import { defineStore } from 'pinia'
import { computed, shallowRef, watch } from 'vue'
import type { LocationQuery } from 'vue-router'
import type { AttachmentReference } from '@/api/attachmentWorkbench'
import type { BusinessSection, CorrectionFilters, CorrectionState, RelationFilters } from '@/api/business'
import type { FamilyTab } from '@/api/families'
import { readerID, readerKey, readReaderQuestion, type ReaderDocument, type ReaderIntent, type ReaderQuestion } from '@/views/document-reader/model'
import type { SourceLocateRequest } from '@/utils/sourceLocator'
import { useAuthStore } from './auth'

const workbenchQueryKeys = new Set([
  'kb', 'tab', 'state', 'title', 'view', 'family', 'attachment', 'attachmentReasons', 'document', 'document_id', 'id',
  'feedbackTab', 'inbox', 'guidance', 'sourceDocument', 'version_id', 'correctionKB', 'correctionState',
  'correction', 'correctionRevision', 'correctionDocument', 'correctionVersion', 'relationDocument', 'relationKB',
  'relationType', 'relationStatus', 'relationEndpoint', 'relationPage', 'mode', 'node', 'associationFrom',
])

export function workbenchTarget(query: LocationQuery, hash: string) {
  return {
    name: 'documents' as const,
    query: Object.fromEntries(Object.entries(query).filter(([key, value]) => workbenchQueryKeys.has(key) && typeof value === 'string')),
    hash,
  }
}

export type ReaderSource =
  | { kind: 'document'; record?: string }
  | { kind: 'family'; family: string; record?: string; revision?: number; historyPreview?: boolean }
  | { kind: 'attachment'; record: string; relation?: string; parentVersion: string; reference: AttachmentReference }
  | { kind: 'correction'; record?: string; revision?: string }
  | { kind: 'business'; section: BusinessSection; record: string; evidence: string }

export interface WorkbenchReaderRequest {
  left: ReaderDocument
  right?: ReaderDocument
  view?: ReaderIntent['view']
  source: ReaderSource
}

export interface FamilyReaderState {
  family: string
  tab: FamilyTab
  familyPage: number
  page: number
  memberPage: number
  scroll: number
}

export interface CorrectionReaderState {
  scopeMode: 'current' | 'all'
  advancedOpen: boolean
  draft: { documentId: string; keyword: string; state: CorrectionState; from: string; to: string }
  filters: CorrectionFilters
  loadedPages: Record<string, number>
  scroll: number
  detailScroll: number
}

export interface BusinessReaderState {
  kb: string
  feedbackTab: 'feedback' | 'guidance' | 'inbox'
  feedbackStatus: string
  relationFilters: RelationFilters
  page: number
  record?: string
  queryDocument: string
  queryScope: string
  expandedRelation: string
  scroll: number
  detailScroll: number
}

export interface WorkbenchReturn extends WorkbenchReaderRequest {
  path: string
  target: ReturnType<typeof workbenchTarget>
  targetPath: string
  kb: string
  area: 'main' | 'drawer'
  document?: { id: string; tab: string; kb: string; scroll: number; activityPage: number }
  scroll: number
  documentScroll: number
  pages: number
  sort: { key: string; direction: number }
  expanded: readonly string[]
  reviewSort: 'asc' | 'desc'
  collapsedTaskSections: ('attachments' | 'reviews')[]
  mainFamily?: FamilyReaderState
  drawerFamily?: FamilyReaderState
  correction?: CorrectionReaderState
  mainBusiness?: BusinessReaderState
  drawerBusiness?: BusinessReaderState
}

interface ReaderTab { doc: ReaderDocument; title?: string }

export interface ReaderCitation {
  id: string
  doc: ReaderDocument
  fileHash: string
  locate: SourceLocateRequest
}

export const useDocumentReaderStore = defineStore('document-reader', () => {
  const auth = useAuthStore()
  const scope = computed(() => JSON.stringify([auth.currentUserId, auth.effectiveTenantId]))
  const tabs = shallowRef<ReaderTab[]>([])
  const workbench = shallowRef<(WorkbenchReturn & { id: string })>()
  const citation = shallowRef<ReaderCitation>()
  const questionOrigins = new Map<string, ReaderQuestion>()

  watch(scope, () => {
    tabs.value = []
    workbench.value = undefined
    citation.value = undefined
    questionOrigins.clear()
  }, { flush: 'sync' })

  function visit(doc: ReaderDocument) {
    const key = readerKey(doc)
    const entry = tabs.value.find(tab => readerKey(tab.doc) === key)
    tabs.value = [...tabs.value.filter(tab => readerKey(tab.doc) !== key), entry ?? { doc }].slice(-12)
  }

  function name(doc: ReaderDocument, title: string) {
    const key = readerKey(doc)
    tabs.value = tabs.value.map(tab => readerKey(tab.doc) === key ? { doc, title } : tab)
  }

  function close(doc: ReaderDocument) {
    tabs.value = tabs.value.filter(tab => readerKey(tab.doc) !== readerKey(doc))
  }

  function captureWorkbench(value: WorkbenchReturn) {
    workbench.value = { ...value, id: crypto.randomUUID() }
    return workbench.value.id
  }

  function takeWorkbench(path: string, id: unknown) {
    const snapshot = workbench.value
    if (snapshot && snapshot.id === id && (snapshot.path === path || snapshot.targetPath === path)) {
      workbench.value = undefined
      return snapshot
    }
  }

  function discardWorkbench(id: string) {
    if (workbench.value?.id === id) workbench.value = undefined
  }

  function captureCitation(doc: ReaderDocument, fileHash: string, locate: SourceLocateRequest) {
    citation.value = { id: crypto.randomUUID(), doc, fileHash, locate }
    return citation.value.id
  }

  function storedQuestionOrigins(): [string, string][] {
    try {
      const value = JSON.parse(sessionStorage.getItem(`weknora:reader-questions:${scope.value}`) || '[]')
      return Array.isArray(value) ? value.filter(row => Array.isArray(row) && readerID(row[0]) && typeof row[1] === 'string').slice(-24) : []
    } catch { return [] }
  }

  function rememberQuestionOrigin(sessionId: string, question: ReaderQuestion) {
    if (!readerID(sessionId)) return
    questionOrigins.set(sessionId, question)
    if (questionOrigins.size > 24) {
      const oldest = questionOrigins.keys().next().value
      if (oldest) questionOrigins.delete(oldest)
    }
    try {
      const entries = storedQuestionOrigins().filter(([id]) => id !== sessionId)
      entries.push([sessionId, JSON.stringify(question)])
      sessionStorage.setItem(`weknora:reader-questions:${scope.value}`, JSON.stringify(entries.slice(-24)))
    } catch { /* In-memory origins are still available. */ }
  }

  function questionOrigin(sessionId: unknown) {
    const id = readerID(sessionId)
    if (id) return questionOrigins.get(id) ?? readReaderQuestion(storedQuestionOrigins().find(row => row[0] === id)?.[1])
  }

  return { scope, tabs, workbench, citation, visit, name, close, captureWorkbench, takeWorkbench, discardWorkbench,
    captureCitation, rememberQuestionOrigin, questionOrigin }
})
