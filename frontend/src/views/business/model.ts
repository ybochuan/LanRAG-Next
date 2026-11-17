import type { BusinessAction, BusinessBody, BusinessKind, BusinessPayload, BusinessRecord } from '@/api/business'

export const statusLabels: Record<string, string> = {
  candidate: '待确认', active: '已生效', linked: '自动登记', revoked: '已撤销', pending_review: '待审核', approved: '已批准',
  rejected: '已拒绝', quarantined: '已隔离', withdrawn: '已撤回', publishing: '发布处理中',
  publish_failed: '发布待恢复', indexed: '已入库，待完成发布', published: '已发布', superseded: '已被替代',
  attachment_cleanup_pending: '附件清理中', attachment_cleanup_retry: '清理待重试', attachment_removed: '已随主件删除',
  attachment_retired: '旧版附件',
  supplement_declared: '已登记', confirmed: '已确认', ignored: '已忽略',
  historical: '历史记录', removed: '已移出',
  scheduled: '待生效', applied: '已废止', needs_review: '需重新复核',
  awaiting_resolution: '待裁决', resolved: '已裁决',
}
export const actionLabels: Record<BusinessAction, string> = {
  relation_declare: '保存关联', relation_confirm: '确认关联', relation_revoke: '撤销关联',
  repeal_create: '提交废止审核', repeal_approve: '批准废止', repeal_cancel: '撤回废止计划',
  clause_create: '提交条款审核', clause_confirm: '确认条款关系', clause_resolve: '记录冲突裁决', clause_revoke: '撤销条款关系',
  feedback_create: '提交反馈', feedback_approve: '批准反馈', feedback_reject: '拒绝反馈', feedback_quarantine: '隔离反馈',
  feedback_activate: '激活指导', feedback_withdraw: '撤回反馈', feedback_sample_review: '抽检复核', guidance_seed: '添加种子指导', guidance_quarantine_topic: '隔离主题',
  revision_create: '创建修订提案', revision_create_binary: '创建二进制修订建议', revision_bind_binary: '绑定修订文件', revision_approve: '批准提案', revision_reject: '拒绝提案', revision_publish: '核对并发布',
  split_create: '创建拆分批次', split_approve: '批准整批拆分', split_reject: '拒绝整批拆分',
}
// A feedback that hit the 20% sample still has a pending review even after it
// was auto-activated. Failed reviews quarantine the staged guidance.
export function sampleReviewPending(row: BusinessRecord): boolean {
  return row.sample_decision === 'pending' && row.status === 'active'
}

export const edgeLabels: Record<string, string> = {
  amends: '修订', interprets: '解释', related_to: '相关', supplements: '补充依赖', attachment_of: '附件归属',
  cites: '引用依据', implements: '实施细则', template_for: '独立模板', repeals: '制度废止',
  exception_to: '条款例外', conflicts_with: '条款冲突',
}

export const relationChoices = [
  { key: 'amends', label: '修订', direction: '修订文件 → 被修订制度' },
  { key: 'interprets', label: '解释', direction: '解释文件 → 被解释制度' },
  { key: 'related_to', label: '相关候选', direction: '源文档 → 关联文档' },
  { key: 'cites', label: edgeLabels.cites, direction: '引用文件 → 被引用的依据' },
  { key: 'implements', label: edgeLabels.implements, direction: '实施细则 → 上位制度' },
  { key: 'template_for', label: edgeLabels.template_for, direction: '独立模板 → 使用该模板的制度' },
  { key: 'repeals', label: edgeLabels.repeals, direction: '废止通知 → 被废止制度' },
  { key: 'exception_to', label: edgeLabels.exception_to, direction: '例外条款 → 通用条款' },
  { key: 'conflicts_with', label: edgeLabels.conflicts_with, direction: '登记两端冲突条款，确认后进入人工裁决' },
]
export const clauseRelation = (type?: string) => type === 'exception_to' || type === 'conflicts_with'
export const clauseDecisions: Record<string, string> = {
  prefer_source: '按源条款处理', prefer_target: '按目标条款处理', contextual: '按裁决说明区分适用条件',
}
export function canActivate(row: BusinessRecord, now: number): boolean {
  return row.status === 'approved' && !!row.ready_at && Number.isFinite(Date.parse(row.ready_at)) && Date.parse(row.ready_at) <= now
}
export function actionsFor(kind: BusinessKind, row: BusinessRecord, canWrite: boolean, now = Date.now()): BusinessAction[] {
  if (!canWrite) return []
  if (kind === 'relation' && clauseRelation(row.edge_type)) {
    if (row.status === 'revoked') return []
    if (row.pins_current === true && row.status === 'pending_review') return ['clause_confirm', 'clause_revoke']
    if (row.pins_current === true && row.status === 'awaiting_resolution') return ['clause_resolve', 'clause_revoke']
    return ['clause_revoke']
  }
  if (kind === 'relation' && row.edge_type === 'repeals') {
    if (row.status === 'pending_review') return ['repeal_approve', 'repeal_cancel']
    if (row.status === 'scheduled' || row.status === 'needs_review') return ['repeal_cancel']
    return []
  }
  if (kind === 'relation') return row.status === 'candidate' ? ['relation_confirm', 'relation_revoke'] : row.status === 'active' ? ['relation_revoke'] : []
  if (kind === 'feedback') {
    if (row.status === 'pending_review') return ['feedback_approve', 'feedback_reject', 'feedback_quarantine']
    if (row.status === 'approved') return [...(canActivate(row, now) ? ['feedback_activate' as const] : []), 'feedback_withdraw', 'feedback_quarantine']
    if (row.status === 'active') return ['feedback_withdraw', 'feedback_quarantine']
  }
  if (kind === 'revision') {
    if (row.record_kind === 'split_batch') return row.status === 'pending_review' ? ['split_approve', 'split_reject'] : []
    if (row.status === 'pending_review' && row.record_kind !== 'split_part') return ['revision_approve', 'revision_reject']
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
  if ((action === 'revision_create' || action === 'split_create') && (!payload.document_id || !payload.guidance_ids?.length || payload.guidance_ids.length > 3 || new Set(payload.guidance_ids).size !== payload.guidance_ids.length)) throw new Error('请选择源文档和 1–3 条生效指导。')
  if (action === 'split_create') {
    const parts = payload.split_parts ?? []
    if (parts.length < 2 || parts.length > 8) throw new Error('请填写 2–8 份拆分文档。')
    const titles = parts.map(part => part.title.trim())
    if (titles.some(title => !title || bytes(title) > 120) || parts.some(part => !part.text.trim())) {
      throw new Error('请填写每份文档的简短标题和完整正文。')
    }
    if (new Set(titles.map(title => title.toLowerCase())).size !== parts.length) throw new Error('分件标题不能重复。')
    if (parts.reduce((size, part) => size + bytes(part.text.replace(/\r\n/g, '\n').trim() + '\n'), 0) > 48000) {
      throw new Error('批次正文过长，请缩短后再提交。')
    }
  }
  if (['relation_declare', 'repeal_create', 'clause_create'].includes(action) && (!payload.document_id || !payload.target_document_id || payload.document_id === payload.target_document_id)) throw new Error('请选择两个不同的文档。')
  if (action === 'repeal_create' && !(Date.parse(payload.effective_at || '') > 0)) throw new Error('请填写废止生效时间。')
  if (action === 'clause_create' && (!clauseRelation(payload.edge_type) || !payload.source_clause || !payload.target_clause ||
      !payload.applies_when?.trim() || bytes(payload.applies_when) > 4000)) throw new Error('请选择两端条款，并填写适用条件。')
  if (action === 'clause_resolve' && (!Object.hasOwn(clauseDecisions, payload.resolution || '') ||
      !payload.text?.trim() || bytes(payload.text) > 4000)) throw new Error('请选择裁决方式，并填写裁决依据。')
  const copied = JSON.parse(JSON.stringify(payload)) as BusinessPayload
  if (copied.source_clause) Object.freeze(copied.source_clause)
  if (copied.target_clause) Object.freeze(copied.target_clause)
  if (copied.guidance_ids) Object.freeze(copied.guidance_ids)
  if (copied.split_parts) {
    copied.split_parts.forEach(part => Object.freeze(part))
    Object.freeze(copied.split_parts)
  }
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
