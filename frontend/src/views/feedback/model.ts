import type { CorrectionDocumentSummary, CorrectionReference, CorrectionState } from '@/api/business'

export const correctionStateLabels: Record<CorrectionState, string> = {
  active: '有效', all: '全部已发布状态', withdrawn: '已撤回', superseded: '已替代', inactive: '已失效',
}

export function correctionRoute(
  kb: string,
  documentId: string,
  versionId: string,
  item?: Pick<CorrectionReference, 'id' | 'revision'>,
  state: CorrectionState = 'active',
) {
  return {
    name: 'documents',
    query: {
      view: 'feedback-materials', kb, document_id: documentId, version_id: versionId,
      correction: item?.id, correctionRevision: item?.revision.toString(),
      correctionState: state === 'active' ? undefined : state,
    },
  }
}

export function correctionRowId(row: Pick<CorrectionDocumentSummary, 'knowledge_base_id' | 'document_id' | 'version_id'>) {
  return 'correction-document-' + encodeURIComponent(JSON.stringify([row.knowledge_base_id, row.document_id, row.version_id]))
}

export function correctionItemId(id: string, revision: number) {
  return 'correction-item-' + encodeURIComponent(id) + '-' + revision
}

export function browseCorrectionState(value: unknown): CorrectionState {
  return value === 'all' || value === 'withdrawn' || value === 'superseded' || value === 'inactive' ? value : 'active'
}

export function approvalBound(day: string, end = false) {
  const time = new Date(day + (end ? 'T23:59:59.999' : 'T00:00:00'))
  return Number.isFinite(time.getTime()) ? time.toISOString() : undefined
}

export function markdownFileName(name: string) {
  const clean = name.replaceAll('\\', '_').replace(/[\/<>:"|?*\x00-\x1f\u007f\u202a-\u202e\u2066-\u2069]/g, '_').trim()
  return clean.toLowerCase().endsWith('.md') && clean.length > 3 ? clean : '反馈文档.md'
}
