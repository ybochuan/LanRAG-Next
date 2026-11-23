import type { BusinessRecord, BusinessResult, CorrectionRevisionReference } from '@/api/business'

export const uploadMarker = 'agentrag_binary_revision_upload'
export const pageSize = 100

const fileTypes = ['pdf', 'docx', 'xlsx', 'pptx']
const nativeHash = /^[a-f0-9]{32}$/

export type RecoveryStage = '上传' | '解析' | '创建提案' | '绑定'

type RevisionCorrection = Pick<CorrectionRevisionReference, 'id' | 'revision' | 'version_id'>

export interface BinaryRevisionRecovery {
  source: string
  correction?: RevisionCorrection
  fileType: string
  size: number
  fingerprint: string
  uploadOperation: string
  createOperation: string
  bindOperation: string
  uploadAttempted: boolean
  target?: { id: string; hash: string }
  proposal?: { id: string; revision: number }
  reparseFrom?: string
}

export interface NativeDocument {
  id: string
  knowledge_base_id: string
  type: string
  file_type: string
  file_size: number
  file_hash: string
  metadata?: Record<string, unknown>
  parse_status: string
  updated_at: string
}

export interface NativePage {
  success?: boolean
  data?: NativeDocument[] | null
  total?: number
  page?: number
  page_size?: number
}

export class RecoveryError extends Error {
  constructor(message: string, readonly key?: string) { super(message) }
}

export async function fingerprint(file: Blob) {
  if (globalThis.crypto?.subtle) {
    const bytes = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    return Array.from(new Uint8Array(bytes), byte => byte.toString(16).padStart(2, '0')).join('')
  }
  // LAN installations can run over HTTP, where SubtleCrypto is unavailable.
  const { sha256 } = await import('js-sha256')
  const hash = sha256.create()
  const chunkSize = 1024 * 1024
  for (let offset = 0; offset < file.size; offset += chunkSize) {
    hash.update(await file.slice(offset, offset + chunkSize).arrayBuffer())
  }
  return hash.hex()
}

export function fileType(file: File) {
  return file.name.split('.').at(-1)?.toLowerCase() ?? ''
}

function operationId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')
}

export async function startRecovery(source: string, file: File, correction?: CorrectionRevisionReference): Promise<BinaryRevisionRecovery> {
  const type = fileType(file)
  if (!fileTypes.includes(type) || file.size === 0) throw new RecoveryError('请选择非空的 PDF、DOCX、XLSX 或 PPTX 文件。', 'workbench.binaryRevision.invalidFile')
  return {
    source,
    ...(correction ? { correction: { id: correction.id, version_id: correction.version_id, revision: correction.revision } } : {}),
    fileType: type, size: file.size, fingerprint: await fingerprint(file),
    uploadOperation: operationId(), createOperation: operationId(), bindOperation: operationId(), uploadAttempted: false,
  }
}

export function recoveryKey(user: string | undefined, tenant: string | number | null | undefined, kb: string) {
  if (!user || !tenant || !kb) return ''
  return 'agentrag:binary-revision:v1:' + [user, tenant, kb].map(value => encodeURIComponent(value)).join(':')
}

export function readRecovery(raw: string | null): BinaryRevisionRecovery | undefined {
  if (!raw) return
  const recovery = JSON.parse(raw) as BinaryRevisionRecovery
  if (!recovery || typeof recovery.source !== 'string' || !recovery.source || !fileTypes.includes(recovery.fileType) ||
    !Number.isSafeInteger(recovery.size) || recovery.size <= 0 || !/^[a-f0-9]{64}$/.test(recovery.fingerprint) ||
    ![recovery.uploadOperation, recovery.createOperation, recovery.bindOperation].every(id => nativeHash.test(id)) ||
    typeof recovery.uploadAttempted !== 'boolean' ||
    (recovery.correction !== undefined && (!recovery.correction || typeof recovery.correction.id !== 'string' || !recovery.correction.id ||
      typeof recovery.correction.version_id !== 'string' || !recovery.correction.version_id ||
      !Number.isSafeInteger(recovery.correction.revision) || recovery.correction.revision < 1)) ||
    (recovery.target && (!recovery.uploadAttempted || !recovery.target.id || !nativeHash.test(recovery.target.hash))) ||
    (recovery.proposal && (!recovery.target || !recovery.proposal.id || recovery.proposal.revision !== 1)) ||
    (recovery.reparseFrom !== undefined && (!recovery.target || typeof recovery.reparseFrom !== 'string' || !recovery.reparseFrom))) {
    throw new RecoveryError('本次恢复记录无法读取。', 'workbench.binaryRevision.recoveryRecordInvalid')
  }
  return recovery
}

export function targetIdentity(row: NativeDocument | undefined, kb: string, recovery: BinaryRevisionRecovery) {
  if (!row?.id || row.id === recovery.source || row.knowledge_base_id !== kb || row.type !== 'file' ||
    row.file_type?.toLowerCase() !== recovery.fileType || row.file_size !== recovery.size || !nativeHash.test(row.file_hash) ||
    row.metadata?.agentrag_binary_revision !== '1' || row.metadata?.[uploadMarker] !== recovery.uploadOperation ||
    (recovery.target && (row.id !== recovery.target.id || row.file_hash !== recovery.target.hash))) {
    throw new RecoveryError('原生文档与本次修订文件不符，已暂停，请在文档列表核对。', 'workbench.binaryRevision.nativeMismatch')
  }
  return { id: row.id, hash: row.file_hash }
}

export async function findUploadedDocument(recovery: BinaryRevisionRecovery, readPage: (page: number) => Promise<NativePage>) {
  const seen = new Set<string>()
  let candidate: NativeDocument | undefined
  let total: number | undefined
  for (let page = 1; ; page++) {
    const result = await readPage(page)
    const rows = result.data ?? []
    total ??= result.total
    if (!result.success || !Number.isSafeInteger(total) || total! < 0 || result.total !== total ||
      result.page !== page || result.page_size !== pageSize || (result.data == null && total !== 0) ||
      !Array.isArray(rows) || rows.length !== Math.min(pageSize, total! - seen.size)) {
      throw new RecoveryError('无法完整核对原生文档列表，请稍后继续核对。', 'workbench.binaryRevision.listIncomplete')
    }
    for (const row of rows) {
      if (!row?.id || seen.has(row.id)) throw new RecoveryError('核对期间原生文档列表发生变化，请稍后继续核对。', 'workbench.binaryRevision.listChanged')
      seen.add(row.id)
      if (row.metadata?.[uploadMarker] !== recovery.uploadOperation) continue
      if (candidate) throw new RecoveryError('本次上传对应多份原生文档，请先在文档列表核对。', 'workbench.binaryRevision.duplicateUpload')
      candidate = row
    }
    if (seen.size === total) return candidate
  }
}

export function checkProposal(rows: BusinessRecord[], recovery: BinaryRevisionRecovery) {
  const proposal = recovery.proposal!
  const row = Array.isArray(rows) && rows.length === 1 ? rows[0] : undefined
  // The native list returns record_kind=revision for a regular proposal.
  // This value predates the frontend's split-only record_kind union.
  const kind: string | undefined = row?.record_kind
  const correction = recovery.correction
  if (!row || row.id !== proposal.id || kind !== 'revision' || row.source_document_id !== recovery.source || row.proposed_patch !== '' ||
    (correction && (row.source_version_id !== correction.version_id || row.guidance_ids?.length !== 1 || row.guidance_ids[0] !== correction.id))) {
    throw new RecoveryError('暂时无法核对原修订提案，请稍后继续核对。', 'workbench.binaryRevision.proposalCheckFailed')
  }
  if (row.target_document_id === recovery.target?.id) return true
  if (row.target_document_id || row.revision !== proposal.revision || row.status !== 'pending_review') {
    throw new RecoveryError('原提案已变化，请在提案列表核对。', 'workbench.binaryRevision.proposalChanged')
  }
  return false
}

export function checkedCreation(result: BusinessResult) {
  if (!result?.id || result.revision !== 1 || result.status !== 'pending_review') return
  return { id: result.id, revision: result.revision }
}

export function httpStatus(cause: unknown) {
  if (!cause || typeof cause !== 'object') return undefined
  const response = cause as { $httpStatus?: number; status?: number }
  return response.$httpStatus ?? response.status
}

export function knownRejection(cause: unknown) {
  return [400, 401, 403, 404, 405, 409, 413, 415, 422].includes(httpStatus(cause) ?? 0)
}
