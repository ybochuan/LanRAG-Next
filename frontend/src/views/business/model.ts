import type { BusinessAction, BusinessBody, BusinessKind, BusinessPayload, BusinessRecord } from '@/api/business'

export const statusLabels: Record<string, string> = {
  candidate: '待确认', active: '已生效', revoked: '已撤销', pending_review: '待审核', approved: '已批准',
  rejected: '已拒绝', quarantined: '已隔离', withdrawn: '已撤回', publishing: '发布处理中',
  publish_failed: '发布待恢复', indexed: '已入库，待完成发布', published: '已发布', superseded: '已被替代',
}
export const actionLabels: Record<BusinessAction, string> = {
  relation_declare: '保存关联', relation_confirm: '确认关联', relation_revoke: '撤销关联',
  feedback_create: '提交反馈', feedback_approve: '批准反馈', feedback_reject: '拒绝反馈', feedback_quarantine: '隔离反馈',
  feedback_activate: '激活指导', feedback_withdraw: '撤回反馈', guidance_seed: '添加种子指导', guidance_quarantine_topic: '隔离主题',
  revision_create: '创建修订提案', revision_approve: '批准提案', revision_reject: '拒绝提案', revision_publish: '核对并发布',
}
export const edgeLabels: Record<string, string> = { amends: '修订', interprets: '解释', related_to: '相关', supplements: '补充依赖' }
export function canActivate(row: BusinessRecord, now: number): boolean {
  return row.status === 'approved' && !!row.ready_at && Number.isFinite(Date.parse(row.ready_at)) && Date.parse(row.ready_at) <= now
}
export function actionsFor(kind: BusinessKind, row: BusinessRecord, canWrite: boolean, now = Date.now()): BusinessAction[] {
  if (!canWrite) return []
  if (kind === 'relation') return row.status === 'candidate' ? ['relation_confirm', 'relation_revoke'] : row.status === 'active' ? ['relation_revoke'] : []
  if (kind === 'feedback') {
    if (row.status === 'pending_review') return ['feedback_approve', 'feedback_reject', 'feedback_quarantine']
    if (row.status === 'approved') return [...(canActivate(row, now) ? ['feedback_activate' as const] : []), 'feedback_withdraw', 'feedback_quarantine']
    if (row.status === 'active') return ['feedback_withdraw', 'feedback_quarantine']
  }
  if (kind === 'revision') {
    if (row.status === 'pending_review') return ['revision_approve', 'revision_reject']
    if (['approved', 'publishing', 'publish_failed', 'indexed'].includes(row.status)) return ['revision_publish']
  }
  return []
}
export interface WriteIntent { action: BusinessAction; body: BusinessBody }
const bytes = (text: string) => new TextEncoder().encode(text).length
export function buildIntent(action: BusinessAction, row: BusinessRecord | undefined, payload: BusinessPayload, operation: string): WriteIntent {
  if (!operation || (row && (!row.id || !Number.isInteger(row.revision) || row.revision < 1))) throw new Error('请重新载入记录。')
  if (['feedback_create', 'guidance_seed', 'revision_create'].includes(action)) {
    if (!payload.text?.trim()) throw new Error('请填写完整内容。')
    if (bytes(payload.text) > (action === 'revision_create' ? 48000 : 16000)) throw new Error('内容过长，请缩短后再提交。')
    if (action !== 'revision_create' && (!payload.topic?.trim() || bytes(payload.topic) > 128)) throw new Error('请填写简短的主题。')
  }
  if (action === 'revision_create' && (!payload.document_id || !payload.guidance_ids?.length || payload.guidance_ids.length > 3 || new Set(payload.guidance_ids).size !== payload.guidance_ids.length)) throw new Error('请选择源文档和 1–3 条生效指导。')
  if (action === 'relation_declare' && (!payload.document_id || !payload.target_document_id || payload.document_id === payload.target_document_id)) throw new Error('请选择两个不同的文档。')
  const copied = JSON.parse(JSON.stringify(payload)) as BusinessPayload
  if (copied.guidance_ids) Object.freeze(copied.guidance_ids)
  return Object.freeze({ action, body: Object.freeze({ operation_id: operation, expected_revision: row?.revision ?? 0, business: Object.freeze({ ...(row ? { id: row.id } : {}), payload: Object.freeze(copied) }) }) })
}
export function httpStatus(error: unknown): number | undefined {
  const value = error as { response?: { status?: number }; $httpStatus?: number }
  return value?.response?.status ?? value?.$httpStatus
}
export function businessError(error: unknown): string {
  const status = httpStatus(error)
  if (status === 403) return '当前账号没有此知识库的操作权限。'
  if (status === 409) return '记录或业务条件已改变，请重新载入。激活还需满足冷静期与主题复核条件。'
  if (status === 400) return '提交内容无效，请检查文档、指导和完整正文。'
  if (status === 404) return '记录已不可访问，请重新载入。'
  return '服务暂不可用，结果尚未确认。请刷新核对，或用原请求重试。'
}
export const date = (value?: string | null) => value ? new Date(value).toLocaleString() : '未设置'
export const shortID = (value?: string) => value ? value.slice(0, 8) : '—'
