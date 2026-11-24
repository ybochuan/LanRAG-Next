<template>
  <section ref="workspace" class="attachment-workbench" aria-labelledby="attachment-workbench-title">
    <header class="attachment-header">
      <h3 id="attachment-workbench-title">{{ $t('workbench.attachmentWorkbench.title') }}</h3>
      <t-space v-if="canWrite && view" size="small" break-line>
        <t-button variant="outline" size="small" :disabled="busy" @click="previewImport">{{ $t('workbench.attachmentWorkbench.actions.extract') }}</t-button>
        <t-button variant="outline" size="small" :disabled="busy" @click="showUpload = true">{{ $t('workbench.attachmentWorkbench.actions.upload') }}</t-button>
        <t-button variant="outline" size="small" :disabled="busy" @click="openPicker">{{ $t('workbench.attachmentWorkbench.actions.link') }}</t-button>
        <t-button v-if="archiveCandidates.length" variant="outline" size="small" :disabled="busy" @click="showArchive = true">{{ $t('workbench.attachmentWorkbench.actions.archive') }}</t-button>
        <t-button variant="text" size="small" :disabled="busy" @click="load">{{ $t('workbench.attachmentWorkbench.actions.refresh') }}</t-button>
      </t-space>
    </header>
    <t-alert v-if="error" theme="error" role="alert">{{ error }}</t-alert>
    <t-alert v-if="message" :theme="messageTheme" role="status">{{ message }}</t-alert>
    <t-loading :loading="busy && !view">
      <t-empty v-if="view && !view.items.length" :description="$t('workbench.attachmentWorkbench.empty')" />
      <div v-for="item in view?.items || []" :key="item.relation_id || item.id" class="attachment-item" role="listitem">
        <div class="attachment-item-header">
          <t-button v-if="!['pending','removed','shared'].includes(item.state)" variant="text" size="small" class="attachment-title-link" @click="router.push(knowledgeRoute(item.id))">{{ item.title || item.file_name }}</t-button>
          <span v-else>{{ item.title || item.file_name }}</span>
          <t-space size="small">
            <t-tag v-if="item.relation_type === 'template_for'" variant="light" size="small">{{ $t('workbench.attachmentWorkbench.tags.template') }}</t-tag>
            <t-tag v-if="item.relation_type === 'template_for' && item.mapping_status === 'manual'" variant="light" theme="success" size="small">{{ $t('workbench.attachmentWorkbench.tags.manual') }}</t-tag>
            <t-tag v-else-if="item.relation_type === 'cites'" variant="light" size="small">{{ $t('workbench.attachmentWorkbench.tags.cites') }}</t-tag>
            <t-tag v-else :theme="item.mapping_status === 'candidate' ? 'warning' : item.mapping_status === 'manual' ? 'success' : 'default'" variant="light" size="small">
              {{ reclaimLabel(item) || states[item.state] || mappings[item.mapping_status] || $t('workbench.attachmentWorkbench.tags.unmatched') }}
            </t-tag>
          </t-space>
        </div>
        <div v-if="item.file_name !== item.title" class="attachment-meta">{{ $t('workbench.attachmentWorkbench.labels.originalFile', { name: item.file_name }) }}</div>
        <div v-if="item.reference.title || item.reference.location" class="attachment-meta">
          {{ $t('workbench.attachmentWorkbench.labels.bodyReference', { value: [item.reference.title, item.reference.location].filter(Boolean).join(' · ') }) }}
        </div>
        <p v-if="item.reference.excerpt" class="attachment-excerpt">{{ item.reference.excerpt }}</p>
        <div v-if="item.title_state === 'pending' || item.title_state === 'conflict'" class="attachment-meta">
          {{ $t(item.title_state === 'pending' ? 'workbench.attachmentWorkbench.labels.titlePending' : 'workbench.attachmentWorkbench.labels.titleConflict') }}
        </div>
        <div v-if="item.parse_status && item.parse_status !== 'completed'" class="attachment-meta">{{ parseLabels[item.parse_status] || item.parse_status }}</div>
        <div v-if="item.reclaim_error" class="attachment-meta" role="status">{{ reclaimErrors[item.reclaim_error] || reclaimErrors.storage_unavailable }}</div>
        <t-space size="small" break-line>
          <t-button v-if="item.state === 'current' && view?.parent_version" :data-reader-attachment="item.relation_id || item.id" variant="text" size="small" :disabled="busy" @click="compare(item)">{{ $t('reader.compare') }}</t-button>
          <t-button v-if="canWrite && item.state === 'current'" variant="text" size="small" :disabled="busy" @click="download(item)">{{ $t('workbench.attachmentWorkbench.actions.download') }}</t-button>
          <t-button v-if="canWrite && (item.managed || item.relation_type === 'template_for') && item.state === 'current'" variant="text" size="small" :disabled="busy" @click="edit(item)">{{ $t('workbench.attachmentWorkbench.actions.edit') }}</t-button>
          <t-button v-if="canWrite && item.managed && item.state === 'detached'" variant="text" size="small" :disabled="busy" @click="change(item, 'restore')">{{ $t('workbench.attachmentWorkbench.actions.restore') }}</t-button>
          <t-button v-if="canWrite && item.state === 'current' && (item.managed || item.relation_id)" variant="text" size="small" :disabled="busy" @click="pendingAction = { item, action: 'detach' }">{{ $t('workbench.attachmentWorkbench.actions.detach') }}</t-button>
          <t-button v-if="canWrite && ['current','detached','historical'].includes(item.state)" theme="danger" variant="text" size="small" :disabled="busy" @click="pendingAction = { item, action: 'delete' }">{{ $t('workbench.attachmentWorkbench.actions.delete') }}</t-button>
        </t-space>
      </div>
    </t-loading>

    <t-dialog v-model:visible="showPreview" :header="$t('workbench.attachmentWorkbench.dialogs.extractTitle')" width="560px" attach="body" :confirm-btn="{ content: selected.some(id => importResults[id]?.error) ? $t('workbench.attachmentWorkbench.dialogs.retrySelected') : $t('workbench.attachmentWorkbench.dialogs.importSelected'), disabled: !selected.length, loading: busy }" @confirm="importSelected">
      <t-form label-align="top">
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.selectAttachment')">
          <t-alert v-if="previewIssues.length" theme="warning" role="status" :message="$t('workbench.attachmentWorkbench.dialogs.partialExtract')">
            <ul>
              <li v-for="(issue, index) in previewIssues" :key="index">{{ issue.file_name }} · {{ extractionLabels[issue.reason] || issue.reason }}</li>
            </ul>
          </t-alert>
          <t-empty v-if="!preview.length && !previewIssues.length" :description="$t('workbench.attachmentWorkbench.dialogs.noPreview')" />
          <t-checkbox-group v-if="preview.length" v-model="selected">
            <div v-for="item in preview" :key="item.fingerprint" class="attachment-option">
              <t-checkbox :value="item.fingerprint" :disabled="busy || item.state === 'removed' || !!importResults[item.fingerprint]?.document_id">
                {{ item.display_name || item.file_name }}
              </t-checkbox>
              <span class="attachment-meta" role="status">{{ item.file_name }} · {{ importLabel(item) }}</span>
            </div>
          </t-checkbox-group>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="showUpload" :header="$t('workbench.attachmentWorkbench.dialogs.uploadTitle')" width="480px" attach="body"
      :confirm-btn="{ content: $t('workbench.attachmentWorkbench.dialogs.upload'), disabled: !uploadFile, loading: busy }" @confirm="upload">
      <t-form label-align="top">
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.selectFile')">
          <t-upload :auto-upload="false" :multiple="false" accept=".docx,.xlsx,.pptx,.pdf" @change="selectUpload" />
        </t-form-item>
        <p class="attachment-meta">{{ $t('workbench.attachmentWorkbench.dialogs.uploadHint') }}</p>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="showPicker" :header="$t('workbench.attachmentWorkbench.dialogs.linkTitle')" width="560px" attach="body"
      :confirm-btn="{ content: $t('workbench.attachmentWorkbench.dialogs.linkAction'), disabled: !picked, loading: busy }" @confirm="linkPicked">
      <t-form label-align="top">
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.searchFiles')">
          <t-space>
            <t-input v-model="pickerQuery" clearable :placeholder="$t('workbench.attachmentWorkbench.dialogs.searchPlaceholder')" @enter="searchFiles" />
            <t-button variant="outline" :loading="searching" @click="searchFiles">{{ $t('workbench.attachmentWorkbench.dialogs.search') }}</t-button>
          </t-space>
        </t-form-item>
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.selectFileLabel')">
          <t-select v-model="picked" :options="pickerOptions" filterable clearable :placeholder="$t('workbench.attachmentWorkbench.dialogs.selectTemplate')" />
        </t-form-item>
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.linkType')">
          <t-select v-model="relationType" :options="relationTypes" />
        </t-form-item>
        <t-checkbox v-if="relationType === 'template_for'" v-model="movePicked">{{ $t('workbench.attachmentWorkbench.dialogs.moveToAttachments') }}</t-checkbox>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="showArchive" :header="$t('workbench.attachmentWorkbench.dialogs.archiveTitle')" width="480px" attach="body"
      :confirm-btn="{ content: $t('workbench.attachmentWorkbench.dialogs.moveToFolder'), disabled: !archiveIDs.length, loading: busy }" @confirm="archive">
      <t-form label-align="top">
        <t-form-item :label="$t('workbench.attachmentWorkbench.dialogs.selectAttachment')">
          <t-checkbox-group v-model="archiveIDs">
            <div v-for="item in archiveCandidates" :key="item.id" class="attachment-option">
              <t-checkbox :value="item.id">{{ item.title || item.file_name }}</t-checkbox>
            </div>
          </t-checkbox-group>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="showEdit" :header="$t('workbench.attachmentWorkbench.dialogs.editTitle')" attach="body"
      :confirm-btn="{ content: $t('workbench.attachmentWorkbench.dialogs.save'), disabled: referenceID === undefined, loading: busy }" @confirm="save">
      <t-form label-align="top">
		<t-alert v-if="editing?.relation_type === 'template_for'" theme="info" :message="$t('workbench.attachmentWorkbench.dialogs.templateRefNote')" />
        <t-form-item :label="$t('workbench.attachmentWorkbench.labels.originalFileField')">{{ editing?.file_name }}</t-form-item>
        <t-form-item :label="$t('workbench.attachmentWorkbench.labels.displayTitle')"><t-input v-model="editTitle" :maxlength="240" /></t-form-item>
        <t-form-item :label="$t('workbench.attachmentWorkbench.labels.parentReference')">
          <t-select v-model="referenceID" :options="referenceOptions" filterable :placeholder="$t('workbench.attachmentWorkbench.dialogs.selectReference')" />
        </t-form-item>
        <p v-if="chosenReference?.excerpt" class="attachment-excerpt">{{ chosenReference.excerpt }}</p>
      </t-form>
    </t-dialog>

    <t-dialog :visible="!!pendingAction" :header="pendingAction?.action === 'delete' ? $t('workbench.attachmentWorkbench.dialogs.deleteTitle') : $t('workbench.attachmentWorkbench.dialogs.detachTitle')" attach="body"
      :confirm-btn="{ content: pendingAction?.action === 'delete' ? $t('workbench.attachmentWorkbench.dialogs.deleteFile') : $t('workbench.attachmentWorkbench.actions.detach'), theme: 'danger', loading: busy }"
      @close="pendingAction = null" @cancel="pendingAction = null" @confirm="executeAction">
      <p>{{ pendingAction?.item.title }}</p>
      <p v-if="pendingAction?.action === 'delete'">{{ $t('workbench.attachmentWorkbench.dialogs.deleteNote') }}</p>
      <p v-else>{{ $t('workbench.attachmentWorkbench.dialogs.detachNote') }}</p>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { UploadFile } from 'tdesign-vue-next'
import { listKnowledgeFiles, moveKnowledgeToFolder, delKnowledgeDetails } from '@/api/knowledge-base'
import { write as writeRelation } from '@/api/business'
import { downloadRelatedAttachment } from '@/api/relatedAttachments'
import { attachmentWorkbench, updateAttachment, uploadWorkbenchAttachment, type AttachmentItem, type AttachmentPreview, type AttachmentImportOutcome, type AttachmentExtractionIssue, type AttachmentWorkbench } from '@/api/attachmentWorkbench'
import { httpStatus } from '../business/model'
import type { ReaderSource, WorkbenchReaderRequest } from '@/stores/documentReader'

const props = defineProps<{ kb: string; documentId: string; canWrite: boolean; parentState?: string; readerSource?: ReaderSource }>()
const emit = defineEmits<{ locked: [value: boolean]; changed: []; read: [request: WorkbenchReaderRequest] }>()
const router = useRouter()
const { t } = useI18n()
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
const workspace = ref<HTMLElement>()
let readerSource = props.readerSource
function readerDirty() {
  return showPreview.value || showUpload.value || showPicker.value || showArchive.value || showEdit.value || !!pendingAction.value
}
defineExpose({readerDirty})
function compare(item: AttachmentItem) {
  const parent = view.value
  if (busy.value || !parent?.parent_version || parent.parent_id !== props.documentId || item.state !== 'current') return
  emit('read', {
    left: { kb: props.kb, id: parent.parent_id }, right: { kb: props.kb, id: item.id },
    source: { kind: 'attachment', record: item.id, relation: item.relation_id, parentVersion: parent.parent_version, reference: { ...item.reference } },
  })
}
async function restoreReaderFocus() {
  const source = readerSource
  readerSource = undefined
  if (source?.kind !== 'attachment') return
  await nextTick()
  if (disposed) return
  const buttons = workspace.value?.querySelectorAll<HTMLElement>('[data-reader-attachment]') || []
  Array.from(buttons).find(button => button.dataset.readerAttachment === (source.relation || source.record))?.focus({ preventScroll: true })
}
const mappings = computed<Record<string, string>>(() => ({
  confirmed: t('workbench.attachmentWorkbench.mappings.confirmed'),
  candidate: t('workbench.attachmentWorkbench.mappings.candidate'),
  unmatched: t('workbench.attachmentWorkbench.mappings.unmatched'),
  manual: t('workbench.attachmentWorkbench.mappings.manual'),
}))
const states = computed<Record<string, string>>(() => ({
  detached: t('workbench.attachmentWorkbench.states.detached'),
  historical: t('workbench.attachmentWorkbench.states.historical'),
  deleting: t('workbench.attachmentWorkbench.states.deleting'),
  pending: t('workbench.attachmentWorkbench.states.pending'),
  removed: t('workbench.attachmentWorkbench.states.removed'),
  shared: t('workbench.attachmentWorkbench.states.shared'),
}))
const parseLabels = computed<Record<string, string>>(() => ({
  pending: t('workbench.attachmentWorkbench.parse.pending'),
  processing: t('workbench.attachmentWorkbench.parse.processing'),
  failed: t('workbench.attachmentWorkbench.parse.failed'),
  deleting: t('workbench.attachmentWorkbench.parse.deleting'),
}))
const previewStates = computed<Record<string, string>>(() => ({
  new: t('workbench.attachmentWorkbench.preview.new'),
  existing: t('workbench.attachmentWorkbench.preview.existing'),
  pending: t('workbench.attachmentWorkbench.preview.pending'),
  removed: t('workbench.attachmentWorkbench.preview.removed'),
}))
const extractionLabels = computed<Record<string, string>>(() => ({
  size_limit: t('workbench.attachmentWorkbench.extraction.sizeLimit'),
  read_failed: t('workbench.attachmentWorkbench.extraction.readFailed'),
  invalid_format: t('workbench.attachmentWorkbench.extraction.invalidFormat'),
  unsupported_format: t('workbench.attachmentWorkbench.extraction.unsupportedFormat'),
}))
const importErrors = computed<Record<string, string>>(() => ({
  upload_failed: t('workbench.attachmentWorkbench.importErrors.uploadFailed'),
  revision_conflict: t('workbench.attachmentWorkbench.importErrors.revisionConflict'),
  pending: t('workbench.attachmentWorkbench.importErrors.pending'),
  rejected: t('workbench.attachmentWorkbench.importErrors.rejected'),
}))
const reclaimErrors = computed<Record<string, string>>(() => ({
  reclaim_requires_review: t('workbench.attachmentWorkbench.reclaimErrors.reclaimRequiresReview'),
  storage_unavailable: t('workbench.attachmentWorkbench.reclaimErrors.storageUnavailable'),
  index_cleanup_unconfirmed: t('workbench.attachmentWorkbench.reclaimErrors.indexCleanupUnconfirmed'),
}))
const reclaimLabels = computed<Record<string, string>>(() => ({
  pending: t('workbench.attachmentWorkbench.reclaim.pending'),
  removed: t('workbench.attachmentWorkbench.reclaim.removed'),
  shared: t('workbench.attachmentWorkbench.reclaim.shared'),
}))

function reclaimLabel(item: AttachmentItem) {
  if (!item.resources_verified) return ''
  return reclaimLabels.value[item.state] || ''
}

function importLabel(item: AttachmentPreview) {
  const result = importResults.value[item.fingerprint]
  if (result?.error) return importErrors.value[result.error] || t('workbench.attachmentWorkbench.importErrors.fallback')
  if (result?.document_id) return t('workbench.attachmentWorkbench.importDone')
  return previewStates.value[item.state] || item.state
}
const relationTypes = computed(() => [
  { label: t('workbench.attachmentWorkbench.relationTypes.template'), value: 'template_for' },
  { label: t('workbench.attachmentWorkbench.relationTypes.cites'), value: 'cites' },
])
const referenceOptions = computed(() => [
  { value: '', label: t('workbench.attachmentWorkbench.reference.none') },
  ...references.value.map(item => ({ value: item.id, label: [item.sequence ? t('workbench.attachmentWorkbench.reference.attachmentSequence', { n: item.sequence }) : '', item.title, item.location].filter(Boolean).join(' · ') })),
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
  if (props.parentState === 'superseded') {
    error.value = t('workbench.attachmentWorkbench.messages.parentSuperseded')
    return
  }
  const status = httpStatus(err)
  error.value = status === 409 ? t('workbench.attachmentWorkbench.messages.conflict')
    : status === 400 ? t('workbench.attachmentWorkbench.messages.invalid') : t('workbench.attachmentWorkbench.messages.generic')
}

async function load() {
  const token = ++revisionToken
  busy.value = true
  error.value = ''
  try {
    const result = await attachmentWorkbench(props.kb, props.documentId)
    if (token === revisionToken) {
      view.value = result
      void restoreReaderFocus()
    }
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
    notice(selected.value.length ? t('workbench.attachmentWorkbench.messages.importPartial') : t('workbench.attachmentWorkbench.messages.importDone'), selected.value.length ? 'warning' : 'success')
  })
}

function selectUpload(files: UploadFile[]) {
  uploadFile.value = files[0]?.raw
}

function upload() {
  return run(async () => {
    if (!view.value || !uploadFile.value) return
    if (uploadFile.value.size >= 20 * 1024 * 1024) { notice(t('workbench.attachmentWorkbench.messages.uploadTooBig'), 'warning'); return }
    await uploadWorkbenchAttachment(props.kb, view.value.parent_id, view.value.parent_version, uploadFile.value)
    showUpload.value = false
    uploadFile.value = undefined
    await reload()
    notice(t('workbench.attachmentWorkbench.messages.uploadDone'))
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
    notice(t('workbench.attachmentWorkbench.messages.movedToAttachments'))
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
      catch { await reload(); notice(t('workbench.attachmentWorkbench.messages.linkedArchiveFailed'), 'warning'); return }
    }
    await reload()
    notice(t('workbench.attachmentWorkbench.messages.linked'))
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
    if (result?.title_state === 'pending') notice(t('workbench.attachmentWorkbench.messages.mappingPending'), 'info')
    else if (result?.title_state === 'conflict') notice(t('workbench.attachmentWorkbench.messages.mappingConflict'), 'warning')
    else notice(t('workbench.attachmentWorkbench.messages.mappingConfirmed'))
  })
}

function change(item: AttachmentItem, action: 'detach' | 'restore') {
  return run(async () => {
    if (item.relation_id) {
      await writeRelation(props.kb, 'relation_revoke', { operation_id: operationID(), expected_revision: item.revision, business: { id: item.relation_id, payload: {} } })
    } else await mutate(item, action)
    pendingAction.value = null
    await reload()
    notice(action === 'detach' ? t('workbench.attachmentWorkbench.messages.detachDone') : t('workbench.attachmentWorkbench.messages.restoreDone'))
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
    notice(t('workbench.attachmentWorkbench.messages.deleteSubmitted'), 'info')
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
.attachment-item { min-width: 0; padding: 10px 0; border-bottom: 1px solid var(--td-component-stroke); overflow-wrap: anywhere; }
.attachment-item:last-child { border-bottom: 0; }
.attachment-item-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; flex-wrap: wrap; min-width: 0; }
.attachment-title-link { padding: 0; height: auto; min-height: 0; font-size: var(--td-font-size-body-medium); font-weight: 500; }
.attachment-option { display: grid; gap: 4px; margin-block: 8px; }
.attachment-meta, .attachment-excerpt { font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.attachment-excerpt { margin-block: 8px; padding-inline-start: 8px; border-inline-start: 2px solid var(--td-component-stroke); }
</style>
