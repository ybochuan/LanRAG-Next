<template>
  <section class="attachment-workbench" aria-labelledby="attachment-workbench-title">
    <header class="attachment-header">
      <h3 id="attachment-workbench-title">附件</h3>
      <t-space v-if="canWrite && view" size="small" break-line>
        <t-button variant="outline" size="small" :disabled="busy" @click="previewImport">提取附件</t-button>
        <t-button variant="outline" size="small" :disabled="busy" @click="showUpload = true">上传附件</t-button>
        <t-button variant="outline" size="small" :disabled="busy" @click="openPicker">关联已有文件</t-button>
        <t-button v-if="archiveCandidates.length" variant="outline" size="small" :disabled="busy" @click="showArchive = true">归档已有附件</t-button>
        <t-button variant="text" size="small" :disabled="busy" @click="load">刷新</t-button>
      </t-space>
    </header>
    <t-alert v-if="error" theme="error" role="alert">{{ error }}</t-alert>
    <t-alert v-if="message" :theme="messageTheme" role="status">{{ message }}</t-alert>
    <t-loading :loading="busy && !view">
      <t-empty v-if="view && !view.items.length" description="暂无附件" />
      <t-card v-for="item in view?.items || []" :key="item.relation_id || item.id" size="small" class="attachment-row">
        <div class="attachment-header">
          <t-link v-if="!['pending','removed','shared'].includes(item.state)" theme="primary" :href="router.resolve(knowledgeRoute(item.id)).href" @click.exact.prevent="router.push(knowledgeRoute(item.id))">{{ item.title || item.file_name }}</t-link>
          <span v-else>{{ item.title || item.file_name }}</span>
          <t-space size="small">
            <t-tag v-if="item.relation_type === 'template_for'" variant="light" size="small">共享模板</t-tag>
            <t-tag v-if="item.relation_type === 'template_for' && item.mapping_status === 'manual'" variant="light" theme="success" size="small">已人工确认</t-tag>
            <t-tag v-else-if="item.relation_type === 'cites'" variant="light" size="small">引用文件</t-tag>
            <t-tag v-else :theme="item.mapping_status === 'candidate' ? 'warning' : item.mapping_status === 'manual' ? 'success' : 'default'" variant="light" size="small">
              {{ reclaimLabel(item) || states[item.state] || mappings[item.mapping_status] || '未匹配' }}
            </t-tag>
          </t-space>
        </div>
        <div v-if="item.file_name !== item.title" class="attachment-meta">原文件 {{ item.file_name }}</div>
        <div v-if="item.reference.title || item.reference.location" class="attachment-meta">
          正文引用 {{ [item.reference.title, item.reference.location].filter(Boolean).join(' · ') }}
        </div>
        <p v-if="item.reference.excerpt" class="attachment-excerpt">{{ item.reference.excerpt }}</p>
        <div v-if="item.title_state === 'pending' || item.title_state === 'conflict'" class="attachment-meta">
          {{ item.title_state === 'pending' ? '名称待同步' : '名称同步冲突，请重新打开后确认' }}
        </div>
        <div v-if="item.parse_status && item.parse_status !== 'completed'" class="attachment-meta">{{ parseLabels[item.parse_status] || item.parse_status }}</div>
        <div v-if="item.reclaim_error" class="attachment-meta" role="status">{{ reclaimErrors[item.reclaim_error] || reclaimErrors.storage_unavailable }}</div>
        <t-space size="small" break-line>
          <t-button v-if="canWrite && item.state === 'current'" variant="text" size="small" :disabled="busy" @click="download(item)">下载</t-button>
          <t-button v-if="canWrite && (item.managed || item.relation_type === 'template_for') && item.state === 'current'" variant="text" size="small" :disabled="busy" @click="edit(item)">确认或修正</t-button>
          <t-button v-if="canWrite && item.managed && item.state === 'detached'" variant="text" size="small" :disabled="busy" @click="change(item, 'restore')">恢复关联</t-button>
          <t-button v-if="canWrite && item.state === 'current' && (item.managed || item.relation_id)" variant="text" size="small" :disabled="busy" @click="pendingAction = { item, action: 'detach' }">解除关联</t-button>
          <t-button v-if="canWrite && ['current','detached','historical'].includes(item.state)" theme="danger" variant="text" size="small" :disabled="busy" @click="pendingAction = { item, action: 'delete' }">从知识库删除</t-button>
        </t-space>
      </t-card>
    </t-loading>

    <t-dialog v-model:visible="showPreview" header="提取附件" :confirm-btn="{ content: selected.some(id => importResults[id]?.error) ? '重试选中附件' : '导入选中附件', disabled: !selected.length, loading: busy }" @confirm="importSelected">
      <t-alert v-if="previewIssues.length" theme="warning" role="status" message="部分附件未提取，原有关联保留。">
        <ul>
          <li v-for="(issue, index) in previewIssues" :key="index">{{ issue.file_name }} · {{ extractionLabels[issue.reason] || issue.reason }}</li>
        </ul>
      </t-alert>
      <t-empty v-if="!preview.length && !previewIssues.length" description="当前主件没有可提取的附件" />
      <t-checkbox-group v-if="preview.length" v-model="selected">
        <div v-for="item in preview" :key="item.fingerprint" class="attachment-option">
          <t-checkbox :value="item.fingerprint" :disabled="busy || item.state === 'removed' || !!importResults[item.fingerprint]?.document_id">
            {{ item.display_name || item.file_name }}
          </t-checkbox>
          <span class="attachment-meta" role="status">{{ item.file_name }} · {{ importLabel(item) }}</span>
        </div>
      </t-checkbox-group>
    </t-dialog>

    <t-dialog v-model:visible="showUpload" header="上传专属附件"
      :confirm-btn="{ content: '上传', disabled: !uploadFile, loading: busy }" @confirm="upload">
      <t-upload :auto-upload="false" :multiple="false" accept=".docx,.xlsx,.pptx,.pdf" @change="selectUpload" />
      <p class="attachment-meta">每份文件须小于 20 MiB，上传后归入知识库的「附件」目录。</p>
    </t-dialog>

    <t-dialog v-model:visible="showPicker" header="关联已有文件"
      :confirm-btn="{ content: '关联', disabled: !picked, loading: busy }" @confirm="linkPicked">
      <t-input v-model="pickerQuery" clearable placeholder="按标题或原文件名搜索" @enter="searchFiles" />
      <t-button variant="outline" size="small" :loading="searching" @click="searchFiles">搜索</t-button>
      <t-select v-model="picked" :options="pickerOptions" filterable clearable placeholder="选择同库模板或正式文档" />
      <t-select v-model="relationType" :options="relationTypes" />
      <t-checkbox v-if="relationType === 'template_for'" v-model="movePicked">同时移入「附件」目录</t-checkbox>
    </t-dialog>

    <t-dialog v-model:visible="showArchive" header="归档已有附件"
      :confirm-btn="{ content: '移入附件目录', disabled: !archiveIDs.length, loading: busy }" @confirm="archive">
      <t-checkbox-group v-model="archiveIDs">
        <div v-for="item in archiveCandidates" :key="item.id" class="attachment-option">
          <t-checkbox :value="item.id">{{ item.title || item.file_name }}</t-checkbox>
        </div>
      </t-checkbox-group>
    </t-dialog>

    <t-dialog v-model:visible="showEdit" header="确认附件映射"
      :confirm-btn="{ content: '保存', disabled: referenceID === undefined, loading: busy }" @confirm="save">
      <t-form label-align="top">
		<t-alert v-if="editing?.relation_type === 'template_for'" theme="info" message="正文引用仅用于当前主件。修改文件显示名称后，其他引用也会使用新名称。" />
        <t-form-item label="原文件">{{ editing?.file_name }}</t-form-item>
        <t-form-item label="显示标题"><t-input v-model="editTitle" :maxlength="240" /></t-form-item>
        <t-form-item label="主件正文引用">
          <t-select v-model="referenceID" :options="referenceOptions" filterable placeholder="请选择主件引用" />
        </t-form-item>
        <p v-if="chosenReference?.excerpt" class="attachment-excerpt">{{ chosenReference.excerpt }}</p>
      </t-form>
    </t-dialog>

    <t-dialog :visible="!!pendingAction" :header="pendingAction?.action === 'delete' ? '从知识库删除文件' : '解除附件关联'"
      :confirm-btn="{ content: pendingAction?.action === 'delete' ? '删除文件' : '解除关联', theme: 'danger', loading: busy }"
      @close="pendingAction = null" @cancel="pendingAction = null" @confirm="executeAction">
      <p>{{ pendingAction?.item.title }}</p>
      <p v-if="pendingAction?.action === 'delete'">将提交原生删除任务。使用中的共享模板须先解除关联。</p>
      <p v-else>保留知识库中的文件。</p>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { UploadFile } from 'tdesign-vue-next'
import { listKnowledgeFiles, moveKnowledgeToFolder, delKnowledgeDetails } from '@/api/knowledge-base'
import { write as writeRelation } from '@/api/business'
import { downloadRelatedAttachment } from '@/api/relatedAttachments'
import { attachmentWorkbench, updateAttachment, uploadWorkbenchAttachment, type AttachmentItem, type AttachmentPreview, type AttachmentImportOutcome, type AttachmentExtractionIssue, type AttachmentWorkbench } from '@/api/attachmentWorkbench'
import { httpStatus } from '../business/model'

const props = defineProps<{ kb: string; documentId: string; canWrite: boolean }>()
const emit = defineEmits<{ (event: 'locked', value: boolean): void; (event: 'changed'): void }>()
const router = useRouter()
const view = ref<AttachmentWorkbench>()
const busy = ref(false)
const error = ref('')
const message = ref('')
const messageTheme = ref<'success' | 'warning' | 'info'>('success')
const showPreview = ref(false)
const showUpload = ref(false)
const showPicker = ref(false)
const showArchive = ref(false)
const archiveIDs = ref<string[]>([])
const showEdit = ref(false)
const preview = ref<AttachmentPreview[]>([])
const previewIssues = ref<AttachmentExtractionIssue[]>([])
const importResults = ref<Record<string, AttachmentImportOutcome>>({})
const selected = ref<string[]>([])
const uploadFile = ref<File>()
const pickerQuery = ref('')
const pickerOptions = ref<{ label: string; value: string }[]>([])
const searching = ref(false)
const picked = ref('')
const movePicked = ref(false)
const relationType = ref('template_for')
const editing = ref<AttachmentItem>()
const editTitle = ref('')
const referenceID = ref<string>()
const references = ref<NonNullable<AttachmentWorkbench['references']>>([])
const pendingAction = ref<{ item: AttachmentItem; action: 'delete' | 'detach' } | null>(null)
const deletingIDs = new Set<string>()
const deletionDeadlines = new Map<string, number>()
let refreshTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
const operationID = () => crypto.randomUUID()
const mappings: Record<string, string> = {
  confirmed: '已自动匹配', candidate: '待确认', unmatched: '未匹配', manual: '已人工确认',
}
const states: Record<string, string> = { detached: '已解除', historical: '历史附件', deleting: '删除中', pending: '原件待回收', removed: '已删除，托管原件已回收', shared: '已删除，原件仍有引用' }
const parseLabels: Record<string, string> = { pending: '等待解析', processing: '解析中', failed: '解析失败', deleting: '删除中' }
const previewStates: Record<string, string> = { new: '可导入', existing: '沿用已入库文件', pending: '待对账', removed: '已移除' }
const extractionLabels: Record<string, string> = { size_limit: '超出大小限制', read_failed: '读取失败', invalid_format: '格式校验失败', unsupported_format: '暂不支持此格式' }
const importErrors: Record<string, string> = { upload_failed: '导入失败，可重试', revision_conflict: '版本或归属已变化，请重新提取', pending: '上传结果待核对，可重试核对', rejected: '文件未被知识库接收，可重试' }
const reclaimErrors: Record<string, string> = { reclaim_requires_review: '资源归属需要核对，回收任务已保留。', storage_unavailable: '存储暂不可用，后台将重试回收。', index_cleanup_unconfirmed: '正在等待索引清理确认。' }

function reclaimLabel(item: AttachmentItem) {
  if (!item.resources_verified) return ''
  return ({ pending: '资源待回收', removed: '已删除，已核验资源已回收', shared: '已删除，共享资源已保留' } as Record<string, string>)[item.state] || ''
}

function importLabel(item: AttachmentPreview) {
  const result = importResults.value[item.fingerprint]
  if (result?.error) return importErrors[result.error] || '导入失败'
  if (result?.document_id) return '已导入'
  return previewStates[item.state] || item.state
}
const relationTypes = [{ label: '共享模板', value: 'template_for' }, { label: '引用正式文件', value: 'cites' }]
const referenceOptions = computed(() => [
  { value: '', label: '无对应正文引用' },
  ...references.value.map(item => ({ value: item.id, label: [item.sequence ? `附件 ${item.sequence}` : '', item.title, item.location].filter(Boolean).join(' · ') })),
])
const chosenReference = computed(() => references.value.find(item => item.id === referenceID.value))
const archiveCandidates = computed(() => (view.value?.items || []).filter(item => item.managed && item.state === 'current' && !item.folder_path))
let revisionToken = 0
watch(busy, value => emit('locked', value))
watch(() => [props.kb, props.documentId], () => {
  view.value = undefined
  deletingIDs.clear()
  deletionDeadlines.clear()
  void load()
}, { immediate: true })
onBeforeUnmount(() => {
  disposed = true
  revisionToken++
  clearTimeout(refreshTimer)
})

function scheduleRefresh() {
  clearTimeout(refreshTimer)
  for (const item of view.value?.items || []) {
    if (item.state === 'removed' || item.state === 'shared' ||
      (item.state !== 'deleting' && item.state !== 'pending' && (deletionDeadlines.get(item.id) || Infinity) < Date.now())) {
      deletingIDs.delete(item.id)
      deletionDeadlines.delete(item.id)
    }
  }
  if (disposed || !(deletingIDs.size || view.value?.items.some(item =>
    item.state === 'deleting' || item.state === 'pending' || item.title_state === 'pending' ||
    ['pending', 'processing'].includes(item.parse_status)))) return
  refreshTimer = setTimeout(async () => {
    if (!busy.value && !document.hidden) {
      const token = revisionToken
      try {
        const result = await attachmentWorkbench(props.kb, props.documentId)
        if (token !== revisionToken || disposed || busy.value) { scheduleRefresh(); return }
        const changed = JSON.stringify(result.items) !== JSON.stringify(view.value?.items)
        view.value = result
        if (changed) emit('changed')
      } catch { /* manual refresh remains available */ }
    }
    scheduleRefresh()
  }, 3000)
}

function fail(err: unknown) {
  const status = httpStatus(err)
  error.value = status === 409 ? '文件、名称或关联已变化，请刷新后重试。删除共享模板前请解除有效引用。'
    : status === 400 ? '请核对选中的附件、文件格式和大小后重试。' : '附件操作未完成，请重试。'
}

async function load() {
  const token = ++revisionToken
  busy.value = true
  error.value = ''
  try {
    const result = await attachmentWorkbench(props.kb, props.documentId)
    if (token === revisionToken) view.value = result
  } catch (err) { if (token === revisionToken) fail(err) }
  finally { if (token === revisionToken) { busy.value = false; scheduleRefresh() } }
}

async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try { await action() } catch (err) { fail(err) }
  finally { busy.value = false; scheduleRefresh() }
}

async function reload() {
  view.value = await attachmentWorkbench(props.kb, props.documentId)
  emit('changed')
}

function notice(text: string, theme: 'success' | 'warning' | 'info' = 'success') {
  message.value = text
  messageTheme.value = theme
}

function previewImport() {
  return run(async () => {
    const result = await attachmentWorkbench(props.kb, props.documentId, true)
    view.value = result
    preview.value = result.preview || []
    previewIssues.value = result.preview_issues || []
    importResults.value = {}
    selected.value = preview.value.filter(item => item.state === 'new').map(item => item.fingerprint)
    showPreview.value = true
  })
}

function importSelected() {
  return run(async () => {
    if (!view.value || !selected.value.length) return
    const { parent_id, parent_version } = view.value
    const result = await updateAttachment(props.kb, parent_id, 'import', 0, { parent_id, parent_version, fingerprints: selected.value }, operationID())
    if (!result.items) throw new Error('attachment_import_result_unavailable')
    const outcomes = result.items
    importResults.value = { ...importResults.value, ...Object.fromEntries(outcomes.map(item => [item.fingerprint, item])) }
    selected.value = outcomes.filter(item => item.error).map(item => item.fingerprint)
    await reload()
    notice(selected.value.length ? '有附件未导入，请查看逐项结果。' : '所选附件已导入。', selected.value.length ? 'warning' : 'success')
  })
}

function selectUpload(files: UploadFile[]) {
  uploadFile.value = files[0]?.raw
}

function upload() {
  return run(async () => {
    if (!view.value || !uploadFile.value) return
    if (uploadFile.value.size >= 20 * 1024 * 1024) { notice('每份附件须小于 20 MiB。', 'warning'); return }
    await uploadWorkbenchAttachment(props.kb, view.value.parent_id, view.value.parent_version, uploadFile.value)
    showUpload.value = false
    uploadFile.value = undefined
    await reload()
    notice('附件已入库，解析进度可在知识库查看。')
  })
}

async function searchFiles() {
  if (searching.value || !view.value) return
  searching.value = true
  try {
    const response = await listKnowledgeFiles(props.kb, { page: 1, page_size: 50, keyword: pickerQuery.value.trim(), parse_status: 'completed' })
    const rows = (response.data || []) as { id: string; title?: string; file_name?: string; metadata?: { is_attachment?: string } }[]
    const excluded = new Set([view.value.parent_id, ...view.value.items.map(item => item.id)])
    pickerOptions.value = rows.filter(row => !excluded.has(row.id) && row.metadata?.is_attachment !== 'true')
      .map(row => ({ value: row.id, label: row.title || row.file_name || row.id }))
  } catch (err) { fail(err) }
  finally { searching.value = false }
}

function openPicker() {
  picked.value = ''
  movePicked.value = false
  showPicker.value = true
  void searchFiles()
}

function archive() {
  return run(async () => {
    const candidates = new Set(archiveCandidates.value.map(item => item.id))
    const ids = archiveIDs.value.filter(id => candidates.has(id))
    if (!ids.length) return
    await moveKnowledgeToFolder(props.kb, ids, '附件')
    archiveIDs.value = []
    showArchive.value = false
    await reload()
    notice('已移入「附件」目录。')
  })
}

function linkPicked() {
  return run(async () => {
    if (!view.value || !picked.value) return
    const template = relationType.value === 'template_for'
    await writeRelation(props.kb, 'relation_declare', { operation_id: operationID(), expected_revision: 0, business: { payload: {
      document_id: template ? picked.value : view.value.parent_id,
      target_document_id: template ? view.value.parent_id : picked.value,
      edge_type: relationType.value,
    } } })
    showPicker.value = false
    if (template && movePicked.value) {
      try { await moveKnowledgeToFolder(props.kb, [picked.value], '附件') }
      catch { await reload(); notice('已建立关联，归档失败，请在知识库重试移动。', 'warning'); return }
    }
    await reload()
    notice('已建立关联。')
  })
}

function edit(item: AttachmentItem) {
  return run(async () => {
    const result = await attachmentWorkbench(props.kb, props.documentId, true)
    view.value = result
    const latest = result.items.find(value => value.id === item.id && value.relation_id === item.relation_id)
    if (!latest || latest.state !== 'current') throw new Error('attachment changed')
    editing.value = latest
    editTitle.value = latest.title
    references.value = result.references || []
    const auto = references.value.find(ref => ref.title === latest.reference.title && ref.location === latest.reference.location)
    referenceID.value = auto?.id ?? (latest.mapping_status === 'manual' && !latest.reference.title && !latest.reference.location ? '' : undefined)
    showEdit.value = true
  })
}

async function mutate(item: AttachmentItem, action: 'confirm' | 'detach' | 'restore') {
  if (!view.value) return
  const { parent_id, parent_version } = view.value
  return updateAttachment(props.kb, parent_id, action, item.revision, {
    parent_id, parent_version, attachment_id: item.id, file_hash: item.file_hash,
    relation_id: item.relation_id, expected_title: item.native_title, ...(action === 'confirm' ? { title: editTitle.value, reference_id: referenceID.value } : {}),
  }, operationID())
}

function save() {
  return run(async () => {
    if (!editing.value || referenceID.value === undefined) return
    const result = await mutate(editing.value, 'confirm')
    showEdit.value = false
    await reload()
    if (result?.title_state === 'pending') notice('映射已确认，名称正在同步。', 'info')
    else if (result?.title_state === 'conflict') notice('映射已确认，名称同步冲突。', 'warning')
    else notice('附件映射已确认。')
  })
}

function change(item: AttachmentItem, action: 'detach' | 'restore') {
  return run(async () => {
    if (item.relation_id) {
      await writeRelation(props.kb, 'relation_revoke', { operation_id: operationID(), expected_revision: item.revision, business: { id: item.relation_id, payload: {} } })
    } else await mutate(item, action)
    pendingAction.value = null
    await reload()
    notice(action === 'detach' ? '关联已解除，文件已保留。' : '已恢复附件关联。')
  })
}

function executeAction() {
  const pending = pendingAction.value
  if (!pending) return
  if (pending.action === 'detach') return change(pending.item, 'detach')
  return run(async () => {
    await delKnowledgeDetails(pending.item.id)
    if (pending.item.relation_type === 'attachment_of') {
      deletingIDs.add(pending.item.id)
      deletionDeadlines.set(pending.item.id, Date.now() + 60_000)
    }
    pendingAction.value = null
    await reload()
    notice('已提交删除任务，请刷新查看回收结果。', 'info')
  })
}

function knowledgeRoute(id: string) {
  return { name: 'knowledgeBaseDetail', params: { kbId: props.kb }, query: { knowledge_id: id } }
}

function download(item: AttachmentItem) {
  return run(async () => {
    await downloadRelatedAttachment({ knowledge_id: item.id, knowledge_base_id: props.kb, parent_document_id: view.value?.parent_id || '', file_name: item.file_name, download_url: `/api/v1/knowledge/${item.id}/download` })
  })
}
</script>

<style scoped>
.attachment-workbench { display: grid; gap: 12px; min-width: 0; }
.attachment-header { display: flex; align-items: center; gap: 8px; justify-content: space-between; flex-wrap: wrap; }
.attachment-header h3 { margin: 0; font-size: var(--td-font-size-title-medium); }
.attachment-row { margin-block: 8px; overflow-wrap: anywhere; }
.attachment-option { display: grid; gap: 4px; margin-block: 8px; }
.attachment-meta, .attachment-excerpt { font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.attachment-excerpt { margin-block: 8px; padding-inline-start: 8px; border-inline-start: 2px solid var(--td-component-stroke); }
</style>
