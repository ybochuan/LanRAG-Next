<template>
  <section class="binary-revision" aria-labelledby="binary-revision-title">
    <div class="section-heading">
      <h2 id="binary-revision-title">正式修订文件</h2>
      <span class="subtle">PDF / DOCX / XLSX / PPTX</span>
    </div>
    <p class="subtle">上传修订后的正式源文件，解析完成后建立修订提案并绑定该文件。</p>
    <t-alert v-if="differentRecovery" role="alert" theme="warning">本知识库已有其他待核对的修订操作。请先在修订发布页完成核对。
      <t-button theme="primary" variant="text" size="small" @click="openRevisions">前往修订发布</t-button>
    </t-alert>
    <form v-else @submit.prevent="submit(false)">
      <p v-if="initialCorrection" class="subtle">源文档 · {{ initialCorrection.title || initialCorrection.document_id }} · 版本 {{ initialCorrection.version_id }}</p>
      <p v-else-if="recovery?.correction" class="subtle">源文档 · {{ recovery.source }} · 版本 {{ recovery.correction.version_id }}</p>
      <DocumentPicker v-else v-model="sourceId" :kb="kb" label="原制度文档" searchable :disabled="disabled || uploading || !!recovery" />
      <p v-if="correction" class="subtle">已选勘误 · {{ correction.id }} · 修订 {{ correction.revision }}</p>
      <label v-if="!recovery?.target">修订后文件
        <t-upload v-model="files" :auto-upload="false" :multiple="false" :max="1" accept=".pdf,.docx,.xlsx,.pptx" :disabled="disabled || uploading" @change="chooseFile" />
      </label>
      <p v-if="file && !recovery?.target" class="subtle">已选择 · {{ file.name }}</p>
      <p v-if="recovery && !recovery.target && !file" class="subtle">可先核对上次上传。需要补传时，请重新选择同一份文件。</p>
      <p v-if="recovery?.target" class="subtle">修订文件标识：{{ recovery.target.id }}</p>
      <p v-if="recovery?.proposal" class="subtle">原提案标识：{{ recovery.proposal.id }}</p>
      <t-alert v-if="error" role="alert" theme="error">{{ error }}</t-alert>
      <t-alert v-if="notice" role="status" theme="success">{{ notice }}</t-alert>
      <div class="recovery-actions">
        <t-button theme="primary" type="submit" :loading="uploading" :disabled="disabled || !scopeKey || storageUnavailable || (!recovery && (!sourceId || !file))">
          {{ uploading ? progress : recovery ? '核对并继续' : '上传并建立建议' }}
        </t-button>
        <t-button v-if="canReparse" type="button" variant="outline" :disabled="disabled || uploading" @click="submit(true)">重新解析已上传文件</t-button>
        <t-popconfirm v-if="recovery && !uploading" content="结束本页恢复？已上传的文件和已创建的提案会保留，请先核对对应列表。" @confirm="endRecovery">
          <t-button type="button" variant="text" :disabled="disabled">结束本页恢复</t-button>
        </t-popconfirm>
      </div>
    </form>
  </section>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { UploadFile } from 'tdesign-vue-next'
import { batchQueryKnowledge, downKnowledgeDetails, listKnowledgeFiles, reparseKnowledge, uploadKnowledgeFile } from '@/api/knowledge-base'
import { list, verifyCorrectionRevision, write, type CorrectionRevisionSource } from '@/api/business'
import { useAuthStore } from '@/stores/auth'
import DocumentPicker from './DocumentPicker.vue'
import {
  checkedCreation, checkProposal, fileType, findUploadedDocument, fingerprint, httpStatus, knownRejection,
  pageSize, readRecovery, recoveryKey, RecoveryError, startRecovery, targetIdentity, uploadMarker,
  type BinaryRevisionRecovery, type NativeDocument, type NativePage, type RecoveryStage,
} from './binaryRevisionRecovery'

const props = defineProps<{ kb: string; disabled: boolean; initialCorrection?: CorrectionRevisionSource }>()
const emit = defineEmits<{ created: []; locked: [value: boolean] }>()
const auth = useAuthStore()
const router = useRouter()
const scopeKey = computed(() => recoveryKey(auth.user?.id, auth.effectiveTenantId, props.kb))
const sourceId = ref('')
const files = ref<UploadFile[]>([])
const file = ref<File>()
const recovery = ref<BinaryRevisionRecovery>()
const correction = computed(() => props.initialCorrection ?? recovery.value?.correction)
const differentRecovery = computed(() => {
  const initial = props.initialCorrection, stored = recovery.value
  return !!initial && !!stored && (stored.source !== initial.document_id || stored.correction?.id !== initial.id ||
    stored.correction.version_id !== initial.version_id || stored.correction.revision !== initial.revision)
})
const uploading = ref(false)
const storageUnavailable = ref(false)
const canReparse = ref(false)
const progress = ref('正在核对…')
const error = ref('')
const notice = ref('')
let run = 0

function stop() {
  run++
  if (uploading.value) {
    uploading.value = false
    emit('locked', false)
  }
}
function openRevisions() {
  void router.push({ name: 'documents', query: { kb: props.kb, view: 'revisions' } })
}

watch([scopeKey, () => props.initialCorrection], ([key, initial]) => {
  stop()
  recovery.value = undefined
  sourceId.value = initial?.document_id ?? ''
  files.value = []
  file.value = undefined
  canReparse.value = false
  storageUnavailable.value = false
  error.value = ''
  notice.value = ''
  if (!key) return
  try {
    recovery.value = readRecovery(sessionStorage.getItem(key))
    sourceId.value = initial?.document_id ?? recovery.value?.source ?? ''
  } catch {
    storageUnavailable.value = true
    error.value = '无法读取本标签页的修订恢复记录。'
  }
}, { immediate: true, flush: 'sync' })
onScopeDispose(stop)

function chooseFile(next: UploadFile[]) {
  file.value = next[0]?.raw
}

function persist(key: string, value?: BinaryRevisionRecovery) {
  try {
    const stored = readRecovery(sessionStorage.getItem(key))
    if (stored?.uploadOperation !== recovery.value?.uploadOperation) {
      throw new RecoveryError('本标签页有其他修订操作，请重新打开面板核对。')
    }
    if (value) sessionStorage.setItem(key, JSON.stringify(value))
    else sessionStorage.removeItem(key)
    recovery.value = value
  } catch (cause) {
    if (cause instanceof RecoveryError) throw cause
    throw new RecoveryError('无法保存修订恢复记录，请检查浏览器的会话存储。')
  }
}

function endRecovery() {
  if (!recovery.value || props.disabled || uploading.value) return
  try {
    persist(scopeKey.value)
    sourceId.value = props.initialCorrection?.document_id ?? ''
    files.value = []
    file.value = undefined
    canReparse.value = false
    error.value = ''
  } catch (cause) {
    if (cause instanceof RecoveryError) error.value = cause.message
  }
}

async function submit(retryParse: boolean) {
  const key = scopeKey.value
  const kb = props.kb
  const selectedFile = file.value
  const initial = props.initialCorrection
  const selectedSource = initial?.document_id ?? sourceId.value
  if (!key || props.disabled || storageUnavailable.value || uploading.value || differentRecovery.value ||
    (!recovery.value && (!selectedSource || !selectedFile))) return
  const token = ++run
  const current = () => token === run && key === scopeKey.value
  function ensureCurrent() {
    if (!current() || props.disabled) throw new RecoveryError('操作范围或权限已变化，请在原知识库继续核对。')
  }
  function save(value: BinaryRevisionRecovery) {
    ensureCurrent()
    persist(key, value)
    operation = value
  }
  async function mutate<T>(request: () => Promise<T>, invalid?: (value: T) => boolean): Promise<T> {
    ensureCurrent()
    try {
      const result = await request()
      if (invalid?.(result)) throw new Error()
      return result
    } catch (cause) {
      if (knownRejection(cause)) throw new RecoveryError(stage + '请求被拒绝（HTTP ' + httpStatus(cause) + '）。请核对后继续。')
      throw new RecoveryError(stage + '请求结果未知。请核对并继续原操作。')
    }
  }
  async function verifyRecoveredUpload(row: NativeDocument) {
    const target = targetIdentity(row, kb, operation)
    ensureCurrent()
    const original = await downKnowledgeDetails(encodeURIComponent(target.id))
    if (original.size !== operation.size || await fingerprint(original) !== operation.fingerprint) {
      throw new RecoveryError('原生文档的原件与本次修订文件不同，已暂停。')
    }
    save({ ...operation, target })
  }
  async function recoverUpload() {
    if (operation.uploadAttempted) {
      const existing = await findUploadedDocument(operation, page => {
        ensureCurrent()
        return listKnowledgeFiles(kb, { page, page_size: pageSize, file_type: operation.fileType, sort_by: 'created_at', sort_order: 'asc' }) as Promise<NativePage>
      })
      if (existing) return verifyRecoveredUpload(existing)
    }
    if (!selectedFile) throw new RecoveryError('本次上传暂未查到对应文档。请重新选择同一份文件后继续。')
    if (operation.uploadAttempted && (fileType(selectedFile) !== operation.fileType || selectedFile.size !== operation.size ||
      await fingerprint(selectedFile) !== operation.fingerprint)) {
      throw new RecoveryError('所选文件与本次修订文件的指纹不符。请重新选择原文件。')
    }
    save({ ...operation, uploadAttempted: true })
    const response = await mutate(async () => {
      try {
        return await uploadKnowledgeFile(kb, {
          file: selectedFile,
          metadata: JSON.stringify({ agentrag_binary_revision: '1', [uploadMarker]: operation.uploadOperation }),
        }) as { success?: boolean; data?: NativeDocument; duplicate?: boolean }
      } catch (cause) {
        const conflict = cause as { code?: string; data?: NativeDocument } | undefined
        if (httpStatus(cause) !== 409 || conflict?.code !== 'duplicate_file') throw cause
        return { duplicate: true, data: conflict.data }
      }
    }, result => !result?.duplicate && (!result?.success || !result.data?.id))
    if (response.duplicate) {
      if (response.data?.metadata?.[uploadMarker] !== operation.uploadOperation) {
        throw new RecoveryError('同库已有内容相同的文件，归属与本次上传不同。请先在文档列表核对。')
      }
      return verifyRecoveredUpload(response.data)
    }
    save({ ...operation, target: targetIdentity(response.data, kb, operation) })
  }
  async function waitForParsed() {
    const id = operation.target!.id
    for (let attempt = 0; attempt < 60; attempt++) {
      ensureCurrent()
      const response = await batchQueryKnowledge('ids=' + encodeURIComponent(id), kb) as { success?: boolean; data?: NativeDocument[] }
      ensureCurrent()
      if (!response?.success || !Array.isArray(response.data)) throw new RecoveryError('暂时无法核对原文件的解析状态，请继续核对。')
      const row = response.data.find(item => item.id === id)
      targetIdentity(row, kb, operation)
      const { parse_status: status, updated_at: updatedAt } = row!
      if (operation.reparseFrom && (status !== 'failed' || (updatedAt && updatedAt !== operation.reparseFrom))) {
        save({ ...operation, reparseFrom: undefined })
      }
      if (status === 'completed') return
      if (status === 'failed') {
        if (operation.reparseFrom) throw new RecoveryError('重新解析的结果仍待核对。请继续核对，或在原生文档列表检查解析任务。')
        if (!retryParse || !updatedAt) {
          canReparse.value = !!updatedAt
          throw new RecoveryError('已上传文件解析失败。可重新解析该文件，再继续本次修订。')
        }
        retryParse = false
        save({ ...operation, reparseFrom: updatedAt })
        await mutate(async () => {
          try {
            return await reparseKnowledge(encodeURIComponent(id)) as { success?: boolean }
          } catch (cause) {
            if (knownRejection(cause)) save({ ...operation, reparseFrom: undefined })
            throw cause
          }
        }, result => !result?.success)
        continue
      }
      if (status === 'deleting' || status === 'cancelled' || status === 'canceled') {
        throw new RecoveryError('原文件已取消解析或正在删除，请在原生文档列表核对。')
      }
      await new Promise(resolve => setTimeout(resolve, 1000))
    }
    throw new RecoveryError('仍在等待原文件解析。请稍后继续核对。')
  }
  async function bindingConfirmed() {
    ensureCurrent()
    const rows = await list(kb, 'revision', 1, operation.proposal!.id)
    ensureCurrent()
    return checkProposal(rows, operation)
  }

  let operation: BinaryRevisionRecovery
  let stage: RecoveryStage = '上传'
  uploading.value = true
  progress.value = '正在核对上传…'
  error.value = ''
  notice.value = ''
  canReparse.value = false
  emit('locked', true)
  try {
    if (recovery.value) operation = recovery.value
    else {
      if (initial) {
        try { await verifyCorrectionRevision(kb, initial) }
        catch { throw new RecoveryError('所选勘误已变化或暂时无法核对，请返回勘误文档重新选择。') }
        ensureCurrent()
      }
      operation = await startRecovery(selectedSource, selectedFile!, initial)
      save(operation)
    }
    if (!operation.target) await recoverUpload()
    stage = '绑定'
    progress.value = '正在核对提案…'
    if (!operation.proposal || !(await bindingConfirmed())) {
      stage = '解析'
      progress.value = '正在核对解析…'
      await waitForParsed()
      if (!operation.proposal) {
        stage = '创建提案'
        progress.value = '正在核对并创建提案…'
        const result = await mutate(() => write(kb, 'revision_create_binary', {
          operation_id: operation.createOperation, expected_revision: 0,
          business: { payload: {
            document_id: operation.source,
            ...(operation.correction ? { guidance_ids: [operation.correction.id] } : {}),
          } },
        }), value => !checkedCreation(value))
        save({ ...operation, proposal: checkedCreation(result)! })
      }
      stage = '绑定'
      progress.value = '正在核对并绑定…'
      if (!await bindingConfirmed()) {
        const { id, revision } = operation.proposal!
        await mutate(() => write(kb, 'revision_bind_binary', {
          operation_id: operation.bindOperation, expected_revision: revision,
          business: { id, payload: { target_document_id: operation.target!.id } },
        }), value => value?.id !== id || value.revision !== revision + 1 || value.target_document_id !== operation.target!.id)
      }
    }
    ensureCurrent()
    persist(key)
    notice.value = '已确认修订提案（' + operation.proposal!.id + '）绑定该文件，请在列表中查看。'
    sourceId.value = props.initialCorrection?.document_id ?? ''
    files.value = []
    file.value = undefined
    emit('created')
  } catch (cause) {
    if (current()) error.value = cause instanceof RecoveryError ? cause.message : '暂时无法核对' + stage + '状态，请稍后继续。'
  } finally {
    if (current()) stop()
  }
}
</script>

<style scoped>
.binary-revision { margin-bottom: 1rem; }
.binary-revision form { display: grid; gap: .75rem; max-width: 42rem; }
.recovery-actions { display: flex; flex-wrap: wrap; gap: .5rem; }
</style>
