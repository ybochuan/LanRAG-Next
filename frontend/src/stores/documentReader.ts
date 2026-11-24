import { defineStore } from 'pinia'
import { computed, shallowRef, watch } from 'vue'
import type { LocationQuery } from 'vue-router'
import type { AttachmentReference } from '@/api/attachmentWorkbench'
import type { BusinessSection, CorrectionFilters, CorrectionState, RelationFilters } from '@/api/business'
import type { FamilyTab } from '@/api/families'
import { readerKey, type ReaderDocument, type ReaderIntent } from '@/views/document-reader/model'
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

export const useDocumentReaderStore = defineStore('document-reader', () => {
  const auth = useAuthStore()
  const scope = computed(() => JSON.stringify([auth.currentUserId, auth.effectiveTenantId]))
  const tabs = shallowRef<ReaderTab[]>([])
  const workbench = shallowRef<(WorkbenchReturn & { id: string })>()

  watch(scope, () => { tabs.value = []; workbench.value = undefined }, { flush: 'sync' })

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

  return { scope, tabs, workbench, visit, name, close, captureWorkbench, takeWorkbench, discardWorkbench }
})
