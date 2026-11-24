<template>
  <div class="correction-workspace">
    <SettingDrawer :visible="!!target" :title="detail?.file_name || $t('workbench.correction.title')" icon="file" width="760px" :min-width="560" :max-width="1080" storage-key="setting-drawer:width:correction-detail" hide-footer @update:visible="onDetailVisible">
      <template #subtitle>
        <span v-if="detail" class="metadata">{{ kbName(detail.knowledge_base_id) }} · {{ detail.document_title }} · {{ $t('workbench.correction.version', { version: detail.version_id }) }}</span>
        <span v-else>{{ $t('workbench.correction.detail') }}</span>
      </template>
      <template #header-actions>
        <t-button v-if="detail && target?.state === 'active' && detail.items.length" variant="outline" size="small" @click="compare">{{ $t('reader.compare') }}</t-button>
        <t-button v-if="detail" variant="outline" size="small" @click="emit('open', detail.document_id, detail.document_title, detail.knowledge_base_id)">{{ $t('workbench.correction.openOriginal') }}</t-button>
        <t-button v-if="detail" variant="outline" size="small" :loading="downloading" @click="download">{{ $t('workbench.correction.downloadMarkdown') }}</t-button>
      </template>
      <div ref="detailBody" class="correction-detail-body">
        <p v-if="detailBusy" role="status">{{ $t('workbench.correction.reading') }}</p>
        <t-alert v-if="detailError" theme="error" role="alert">
          {{ detailError }} <t-button variant="text" size="small" @click="loadDetail">{{ $t('workbench.correction.reload') }}</t-button>
        </t-alert>
        <template v-if="detail">
          <t-alert v-if="target?.item && !selectedItem" theme="info" role="status">{{ $t('workbench.correction.itemHidden') }}</t-alert>
          <article v-if="selectedItem" :id="correctionItemId(selectedItem.id, selectedItem.revision)" ref="itemHeading" class="selected-item" tabindex="-1" :aria-label="$t('workbench.correction.locatedAria')">
            <h3>{{ $t('workbench.correction.itemTitle', { id: selectedItem.id, revision: selectedItem.revision }) }}</h3>
            <p class="metadata">{{ $t('workbench.correction.chunk', { chunk: selectedItem.chunk_id }) }}</p>
            <p v-if="selectedItem.applies_when">{{ $t('workbench.correction.appliesWhen', { value: selectedItem.applies_when }) }}</p>
            <p v-if="selectedItem.applies_project">{{ $t('workbench.correction.appliesProject', { value: selectedItem.applies_project }) }}</p>
            <p>{{ selectedItem.correction }}</p>
          </article>
          <div class="correction-markdown markdown-content" v-html="preview" />
          <section v-if="canWriteDetail && detail.items.some(item => item.state==='active')" :aria-label="$t('workbench.correction.relatedRevisions')">
            <h3>{{ $t('workbench.correction.relatedRevisions') }}</h3>
            <ul class="revision-links">
              <li v-for="item in detail.items.filter(item => item.state==='active')" :key="correctionItemId(item.id, item.revision)">
                <t-button variant="text" size="small" @click="router.push(revisionRoute(item))">{{ $t('workbench.correction.createRevision', { id: item.id }) }}</t-button>
              </li>
            </ul>
          </section>
        </template>
      </div>
    </SettingDrawer>
    <section class="correction-list-panel workspace-list-panel" :aria-label="$t('workbench.workspace.correctionDocuments')" :aria-busy="listBusy">
      <div class="workspace-toolbar correction-toolbar">
        <span ref="listHeading" tabindex="-1" class="workspace-toolbar-count">{{ $t('workbench.correction.total', { n: rows.length }) }}</span>
        <div class="workspace-toolbar-actions">
          <t-input v-model="draft.keyword" class="correction-search" clearable :maxlength="160" :placeholder="$t('workbench.correction.searchPlaceholder')" />
          <t-select v-model="draft.state" class="correction-state" :options="stateOptions" />
          <t-button variant="outline" :aria-expanded="advancedOpen" @click="advancedOpen=!advancedOpen">{{ $t('workbench.correction.moreFilters') }}</t-button>
          <t-button theme="primary" @click="applyFilters">{{ $t('workbench.correction.filter') }}</t-button>
          <t-button v-if="nextScopes.length" variant="outline" :loading="listBusy" @click="loadPages(nextScopes)">{{ $t('workbench.correction.loadMore') }}</t-button>
        </div>
      </div>
      <div v-if="advancedOpen" class="correction-filters-advanced">
        <label>{{ $t('workbench.correction.kbScope') }}
          <t-select v-model="scopeMode" :options="scopeOptions" />
        </label>
        <label>{{ $t('workbench.correction.sourceId') }}
          <t-input v-model="draft.documentId" clearable :maxlength="160" />
        </label>
        <label>{{ $t('workbench.correction.approvedFrom') }}
          <t-date-picker v-model="draft.from" value-type="YYYY-MM-DD" format="YYYY-MM-DD" clearable />
        </label>
        <label>{{ $t('workbench.correction.approvedTo') }}
          <t-date-picker v-model="draft.to" value-type="YYYY-MM-DD" format="YYYY-MM-DD" clearable />
        </label>
      </div>
      <t-alert v-if="filterError" theme="warning" role="alert">{{ filterError }}</t-alert>
      <t-alert v-if="failedScopes.length" theme="error" role="alert">
        {{ $t('workbench.correction.failedScopes', { scopes: failedScopes.map(kbName).join('、') }) }}
        <t-button variant="text" size="small" :disabled="listBusy" @click="loadPages(failedScopes)">{{ $t('workbench.correction.retry') }}</t-button>
      </t-alert>
      <p v-if="historyUnavailable" role="status" class="metadata">{{ $t('workbench.correction.historyUnavailable') }}</p>
      <p v-if="listBusy" role="status">{{ $t('workbench.correction.loading') }}</p>
      <div v-if="rows.length" ref="listScroller" class="workspace-list-scroll">
        <div class="workspace-list-table" role="table" :aria-label="$t('workbench.correction.listAria')">
          <div class="workspace-list-head" role="row">
            <span role="columnheader">{{ $t('workbench.correction.colSource') }}</span>
            <span role="columnheader" class="workspace-head-kb">{{ $t('workbench.correction.colKb') }}</span>
            <span role="columnheader" class="workspace-head-version">{{ $t('workbench.correction.colVersion') }}</span>
            <span role="columnheader" class="workspace-head-count">{{ $t('workbench.correction.colActive') }}</span>
            <span role="columnheader" class="workspace-head-date">{{ $t('workbench.correction.colLastApproved') }}</span>
          </div>
          <div class="workspace-list-body" role="rowgroup">
            <div v-for="row in rows" :key="row.rowId" class="workspace-list-row correction-row" role="row">
              <div class="workspace-cell workspace-cell-main" role="cell">
                <div class="workbench-title-line">
                  <t-button :id="row.rowId" variant="text" size="small" class="workbench-row-title" @click="router.push(detailRoute(row))">{{ row.document_title }}</t-button>
                </div>
                <span class="workspace-compact-meta">
                  <span>{{ kbName(row.knowledge_base_id) }}</span>
                  <span>{{ $t('workbench.correction.versionShort', { version: row.version_id }) }}</span>
                  <span>{{ $t('workbench.correction.activeCount', { n: row.active_count }) }}</span>
                  <span>{{ formatDate(row.last_approved_at) }}</span>
                </span>
              </div>
              <div class="workspace-cell correction-kb" role="cell">{{ kbName(row.knowledge_base_id) }}</div>
              <div class="workspace-cell correction-version" role="cell"><span class="workspace-ellipsis">{{ row.version_id }}</span></div>
              <div class="workspace-cell correction-count" role="cell">{{ row.active_count }}</div>
              <div class="workspace-cell correction-date" role="cell">{{ formatDate(row.last_approved_at) }}</div>
            </div>
          </div>
        </div>
      </div>
      <div v-else-if="!listBusy && !failedScopes.length && !historyUnavailable" class="workspace-list-empty-state">
        <t-empty :description="$t('workbench.correction.empty')" />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import DOMPurify from 'dompurify'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { correctionDocument, correctionDocuments, correctionMarkdown, type CorrectionDocument, type CorrectionDocumentSummary, type CorrectionFilters, type CorrectionItem, type CorrectionState } from '@/api/business'
import { authority } from '@/api/lifecycle'
import { renderDocumentPreviewMarkdown } from '@/utils/documentPreviewMarkdown'
import { requestFence } from '../lifecycle/model'
import { approvalBound, browseCorrectionState, correctionItemId, correctionRoute, correctionRowId, correctionStateKeys, markdownFileName } from './model'
import { saveBusinessDownload } from '@/utils/desktopDownload'
import type { CorrectionReaderState, WorkbenchReaderRequest } from '@/stores/documentReader'

const props = defineProps<{ scope: string; knowledgeBases: { id: string; name: string }[]; readerReturn?: CorrectionReaderState }>()
const emit = defineEmits<{ open: [id: string, title: string, kb: string]; read: [request: WorkbenchReaderRequest] }>()
const route = useRoute(), router = useRouter()
const { t, locale } = useI18n()
const routeText = (key: string) => typeof route.query[key] === 'string' ? route.query[key] : ''
const kbName = (kb: string) => props.knowledgeBases.find(row => row.id === kb)?.name || kb
const formatDate = (value: string) => value ? new Date(value).toLocaleString(locale.value) : '—'
const scopeMode = ref<'current' | 'all'>(props.readerReturn?.scopeMode || 'current')
const scopeOptions = computed(() => [
  { value: 'current', label: kbName(props.scope) }, { value: 'all', label: t('workbench.correction.allKbs') },
])
const stateOptions = computed(() => (Object.keys(correctionStateKeys) as CorrectionState[]).map(value => ({ value, label: t(correctionStateKeys[value]) })))
const advancedOpen = ref(props.readerReturn?.advancedOpen || false)
const draft = reactive(props.readerReturn ? { ...props.readerReturn.draft } : { documentId: routeText('sourceDocument'), keyword: '', state: 'active' as CorrectionState, from: '', to: '' })
const filters = ref<CorrectionFilters>(props.readerReturn ? { ...props.readerReturn.filters } : { state: 'active', document_id: draft.documentId })
const listFence = requestFence(), detailFence = requestFence(), downloadFence = requestFence()
const listBusy = ref(false), filterError = ref(''), historyUnavailable = ref(false)
const pages = ref<Record<string, number>>({}), failedScopes = ref<string[]>([])
const loadedPages = ref<Record<string, number>>({})
let listReturn = props.readerReturn
let detailReturn = props.readerReturn
let listGeneration = 0
const records = ref<CorrectionDocumentSummary[]>([])
const scopes = computed(() => props.knowledgeBases.map(kb => kb.id).filter(kb => scopeMode.value==='all' || kb===props.scope))
const nextScopes = computed(() => scopes.value.filter(kb => pages.value[kb] && !failedScopes.value.includes(kb)))
const rows = computed(() => records.value.map(row => ({ ...row, rowId: correctionRowId(row) })))
const target = computed(() => {
  const document = routeText('document_id'), version = routeText('version_id')
  if (!document || !version) return undefined
  return {
    kb: routeText('correctionKB') || routeText('kb') || props.scope, document, version,
    state: browseCorrectionState(route.query.correctionState), item: routeText('correction'), revision: routeText('correctionRevision'),
  }
})
const targetAuthorized = computed(() => props.knowledgeBases.some(kb => kb.id === target.value?.kb))
const detail = ref<CorrectionDocument>(), detailBusy = ref(false), detailError = ref(''), canWriteDetail = ref(false)
const downloading = ref(false)
const listHeading = ref<HTMLElement>(), detailHeading = ref<HTMLElement>(), itemHeading = ref<HTMLElement>()
const listScroller = ref<HTMLElement>(), detailBody = ref<HTMLElement>()
function readerState(): CorrectionReaderState {
  return {
    scopeMode:scopeMode.value, advancedOpen:advancedOpen.value, draft:{...draft}, filters:{...filters.value}, loadedPages:{...loadedPages.value},
    scroll:listScroller.value?.scrollTop || 0, detailScroll:detailBody.value?.closest('.t-drawer__body')?.scrollTop || 0,
  }
}
defineExpose({readerState})

function compare() {
  const current = detail.value
  if (!current || target.value?.state !== 'active' || !current.items.length) return
  const left = { kb: current.knowledge_base_id, id: current.document_id, version: current.version_id }
  const item = selectedItem.value
  emit('read', { left, right: { ...left, kind:'correction' }, source:{ kind:'correction', record:item?.id, revision:item && String(item.revision) } })
}
const selectedItem = computed(() => detail.value?.items.find(item => item.id === target.value?.item && String(item.revision) === target.value.revision))
const preview = computed(() => detail.value ? renderDocumentPreviewMarkdown(detail.value.markdown, html => DOMPurify.sanitize(html, {
  ALLOWED_TAGS: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'br', 'hr', 'ul', 'ol', 'li', 'blockquote', 'pre', 'code', 'strong', 'em', 'del', 'table', 'thead', 'tbody', 'tr', 'th', 'td'],
  ALLOWED_ATTR: [], ALLOW_ARIA_ATTR: false, ALLOW_DATA_ATTR: false,
})) : '')

async function restartList() {
  const generation = ++listGeneration
  listFence.invalidate()
  records.value = []
  pages.value = Object.fromEntries(scopes.value.map(kb => [kb, 1]))
  loadedPages.value = {}
  failedScopes.value = []
  historyUnavailable.value = false
  listBusy.value = false
  if (!scopes.value.length) return
  const saved = listReturn
  listReturn = undefined
  await loadPages(scopes.value)
  if (!saved || generation !== listGeneration) return
  // Re-fetch only pages that had loaded, under the live scope and current permissions.
  let pending = nextScopes.value.filter(kb => pages.value[kb]! <= (saved.loadedPages[kb] || 0))
  while (pending.length && generation === listGeneration) {
    await loadPages(pending)
    pending = nextScopes.value.filter(kb => pages.value[kb]! <= (saved.loadedPages[kb] || 0))
  }
  await nextTick()
  if (generation === listGeneration && listScroller.value) listScroller.value.scrollTop = saved.scroll
}

async function loadPages(requested: string[]) {
  if (listBusy.value) return
  const token = listFence.begin(), currentFilters = filters.value
  const pending = requested.filter(kb => scopes.value.includes(kb) && pages.value[kb])
  listBusy.value = true
  const results = await Promise.allSettled(pending.map(async kb => {
    const page = pages.value[kb]!
    if (currentFilters.state !== 'active' && !(await authority(kb)).can_write) return undefined
    const result = await correctionDocuments(kb, currentFilters, page)
    if (result.page !== page || result.page_size < 1) throw new Error('invalid_correction_page')
    return result
  }))
  if (!listFence.current(token)) return
  const failed = new Set(failedScopes.value)
  const byIdentity = new Map(records.value.map(row => [correctionRowId(row), row]))
  results.forEach((result, index) => {
    const kb = pending[index]!
    if (result.status === 'rejected') {
      failed.add(kb)
      return
    }
    failed.delete(kb)
    const response = result.value
    if (response) loadedPages.value[kb] = response.page
    pages.value[kb] = response && response.page * response.page_size < response.total ? response.page + 1 : 0
    for (const row of response?.items ?? []) {
      if (row.knowledge_base_id === kb) byIdentity.set(correctionRowId(row), row)
    }
  })
  failedScopes.value = [...failed]
  records.value = [...byIdentity.values()].sort((a, b) => b.last_approved_at.localeCompare(a.last_approved_at) || correctionRowId(a).localeCompare(correctionRowId(b)))
  historyUnavailable.value = pending.length === scopes.value.length && pending.length > 0 && results.every(result => result.status === 'fulfilled' && !result.value)
  listBusy.value = false
}

function applyFilters() {
  if (draft.from && draft.to && draft.from > draft.to) {
    filterError.value = t('workbench.correction.filterDateError')
    return
  }
  filterError.value = ''
  filters.value = {
    document_id: draft.documentId.trim(), query: draft.keyword.trim(), state: draft.state,
    approved_from: approvalBound(draft.from), approved_to: approvalBound(draft.to, true),
  }
  restartList()
}

function detailRoute(row: CorrectionDocumentSummary) {
  const to = correctionRoute(props.scope, row.document_id, row.version_id, undefined, filters.value.state)
  return { ...to, query: { ...to.query, correctionKB: row.knowledge_base_id, sourceDocument: routeText('sourceDocument') || undefined } }
}

function revisionRoute(item: CorrectionItem) {
  return {
    name: 'documents', query: {
      view: 'revisions', kb: detail.value?.knowledge_base_id, guidance: item.id,
      correctionDocument: item.document_id, correctionVersion: item.version_id, correctionRevision: String(item.revision),
    },
  }
}

function onDetailVisible(value: boolean) {
  if (!value) void returnToList()
}

async function returnToList() {
  const current = target.value
  const query = { kb: props.scope, view: 'feedback-materials', sourceDocument: routeText('sourceDocument') || undefined }
  await router.replace({ name: 'documents', query })
  await nextTick()
  const trigger = current && document.getElementById(correctionRowId({ knowledge_base_id: current.kb, document_id: current.document, version_id: current.version }))
  const focusTarget = trigger || listHeading.value
  focusTarget?.focus()
}

async function loadDetail() {
  const token = detailFence.begin(), current = target.value
  downloadFence.invalidate()
  detail.value = undefined
  detailError.value = ''
  canWriteDetail.value = false
  downloading.value = false
  detailBusy.value = !!current
  if (!current) return
  await nextTick()
  if (!detailFence.current(token)) return
  detailHeading.value?.focus()
  try {
    if (!targetAuthorized.value) throw new Error('correction_scope_unavailable')
    const permission = authority(current.kb).then(result => result.can_write).catch(() => false)
    if (current.state !== 'active' && !(await permission)) throw new Error('correction_history_unavailable')
    if (!detailFence.current(token)) return
    const result = await correctionDocument(current.kb, current.document, current.version, current.state)
    if (!detailFence.current(token)) return
    if (!matchesTarget(result, current)) throw new Error('correction_scope_unavailable')
    detail.value = result
    await nextTick()
    if (detailFence.current(token)) {
      const saved = detailReturn
      detailReturn = undefined
      const scroller = detailBody.value?.closest('.t-drawer__body')
      if (saved && scroller) scroller.scrollTop = saved.detailScroll
      itemHeading.value?.focus({ preventScroll: !!saved })
    }
    const canWrite = await permission
    if (detailFence.current(token)) canWriteDetail.value = canWrite
  } catch {
    if (detailFence.current(token)) detailError.value = t('workbench.correction.detailError')
  } finally {
    if (detailFence.current(token)) detailBusy.value = false
  }
}

function matchesTarget(result: CorrectionDocument, current: NonNullable<typeof target.value>) {
  return result.knowledge_base_id === current.kb && result.document_id === current.document && result.version_id === current.version
    && result.items.every(item => item.document_id === current.document && item.version_id === current.version
      && (current.state === 'all' || item.state === current.state))
}

async function download() {
  const current = target.value
  if (!current || !detail.value || !targetAuthorized.value || downloading.value) return
  const token = downloadFence.begin()
  downloading.value = true
  detailError.value = ''
  try {
    const result = await correctionMarkdown(current.kb, current.document, current.version, current.state)
    if (!downloadFence.current(token)) return
    if (result.knowledge_base_id !== current.kb || result.document_id !== current.document || result.version_id !== current.version) {
      throw new Error('correction_scope_unavailable')
    }
    const fileName = markdownFileName(result.file_name)
    await saveBusinessDownload(new Blob([result.markdown], { type: 'text/markdown;charset=utf-8' }), {
      fileName,
      sourceType: 'knowledge_document',
      sourceId: current.document,
      sourceTitle: result.file_name || fileName,
      link: `/platform/knowledge-bases/${encodeURIComponent(current.kb)}?knowledge_id=${encodeURIComponent(current.document)}`,
    })
  } catch {
    if (downloadFence.current(token)) {
      detail.value = undefined
      detailError.value = t('workbench.correction.downloadError')
    }
  } finally {
    if (downloadFence.current(token)) downloading.value = false
  }
}

watch(scopes, restartList, { immediate: true, flush: 'sync' })
watch(() => routeText('sourceDocument'), source => {
  draft.documentId = source
  applyFilters()
})
watch([target, targetAuthorized], loadDetail, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { listGeneration++; listFence.invalidate(); detailFence.invalidate(); downloadFence.invalidate() })
</script>

<style scoped>
.correction-workspace {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}
.correction-detail-body {
  min-width: 0;
  overflow-wrap: anywhere;
}
.correction-list-panel {
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.correction-detail-body h3 { font-size: var(--td-font-size-title-medium); }
.correction-toolbar .correction-search { width: 200px; }
.correction-toolbar .correction-state { width: 140px; }
.correction-filters-advanced {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  flex: none;
  padding: 8px 0;
  border-bottom: 1px solid var(--td-component-stroke);
}
.correction-filters-advanced label {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 6px;
  margin: 0;
  color: var(--td-text-color-secondary);
  font-size: var(--td-font-size-body-medium);
  white-space: nowrap;
}
.correction-filters-advanced :deep(.t-date-picker),
.correction-filters-advanced :deep(.t-select) { width: 160px; }
.detail-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.metadata { color: var(--td-text-color-secondary); }
.correction-markdown { margin-block: 16px; overflow-x: auto; }
.correction-markdown :deep(pre) { white-space: pre-wrap; }
.selected-item {
  background: var(--td-bg-color-secondarycontainer);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-medium);
  padding: 12px;
}
.selected-item h3 { margin-top: 0; }
.selected-item p:last-child { margin-bottom: 0; }
.revision-links { padding-inline-start: 20px; }
.correction-workspace a { color: var(--td-brand-color); }
</style>
