<template>
  <div class="correction-workspace">
    <SettingDrawer :visible="!!target" :title="detail?.file_name || '勘误文档'" icon="file" width="760px" :min-width="560" :max-width="1080" storage-key="setting-drawer:width:correction-detail" hide-footer @update:visible="onDetailVisible">
      <template #subtitle>
        <span v-if="detail" class="metadata">{{ kbName(detail.knowledge_base_id) }} · {{ detail.document_title }} · 版本 {{ detail.version_id }}</span>
        <span v-else>勘误详情</span>
      </template>
      <template #header-actions>
        <t-button v-if="detail" variant="outline" size="small" @click="emit('open', detail.document_id, detail.document_title, detail.knowledge_base_id)">打开原文详情</t-button>
        <t-button v-if="detail" variant="outline" size="small" :loading="downloading" @click="download">下载 Markdown</t-button>
      </template>
      <div class="correction-detail-body">
        <p v-if="detailBusy" role="status">正在读取勘误文档…</p>
        <t-alert v-if="detailError" theme="error" role="alert">
          {{ detailError }} <t-button variant="text" size="small" @click="loadDetail">重新载入</t-button>
        </t-alert>
        <template v-if="detail">
          <t-alert v-if="target?.item && !selectedItem" theme="info" role="status">所引用的勘误修订在当前权限和筛选范围内不可见。</t-alert>
          <article v-if="selectedItem" :id="correctionItemId(selectedItem.id, selectedItem.revision)" ref="itemHeading" class="selected-item" tabindex="-1" aria-label="定位到的勘误条目">
            <h3>勘误 {{ selectedItem.id }} · 修订 {{ selectedItem.revision }}</h3>
            <p class="metadata">分块 {{ selectedItem.chunk_id }}</p>
            <p v-if="selectedItem.applies_when">适用条件：{{ selectedItem.applies_when }}</p>
            <p v-if="selectedItem.applies_project">适用项目：{{ selectedItem.applies_project }}</p>
            <p>{{ selectedItem.correction }}</p>
          </article>
          <div class="correction-markdown markdown-content" v-html="preview" />
          <section v-if="canWriteDetail && detail.items.some(item => item.state==='active')" aria-label="关联正式修订">
            <h3>关联正式修订</h3>
            <ul class="revision-links">
              <li v-for="item in detail.items.filter(item => item.state==='active')" :key="correctionItemId(item.id, item.revision)">
                <t-button variant="text" size="small" @click="router.push(revisionRoute(item))">使用勘误 {{ item.id }} 创建修订提案</t-button>
              </li>
            </ul>
          </section>
        </template>
      </div>
    </SettingDrawer>
    <section class="correction-list-panel workspace-list-panel" :aria-label="$t('workbench.workspace.correctionDocuments')" :aria-busy="listBusy">
      <div class="workspace-toolbar correction-toolbar">
        <span ref="listHeading" tabindex="-1" class="workspace-toolbar-count">共 {{ rows.length }} 份</span>
        <div class="workspace-toolbar-actions">
          <t-input v-model="draft.keyword" class="correction-search" clearable :maxlength="160" placeholder="搜索关键词或编号" />
          <t-select v-model="draft.state" class="correction-state" :options="stateOptions" />
          <t-button variant="outline" :aria-expanded="advancedOpen" @click="advancedOpen=!advancedOpen">更多筛选</t-button>
          <t-button theme="primary" @click="applyFilters">筛选</t-button>
          <t-button v-if="nextScopes.length" variant="outline" :loading="listBusy" @click="loadPages(nextScopes)">加载更多</t-button>
        </div>
      </div>
      <div v-if="advancedOpen" class="correction-filters-advanced">
        <label>知识库范围
          <t-select v-model="scopeMode" :options="scopeOptions" />
        </label>
        <label>原文编号
          <t-input v-model="draft.documentId" clearable :maxlength="160" />
        </label>
        <label>审核日期起
          <t-date-picker v-model="draft.from" value-type="YYYY-MM-DD" format="YYYY-MM-DD" clearable />
        </label>
        <label>审核日期止
          <t-date-picker v-model="draft.to" value-type="YYYY-MM-DD" format="YYYY-MM-DD" clearable />
        </label>
      </div>
      <t-alert v-if="filterError" theme="warning" role="alert">{{ filterError }}</t-alert>
      <t-alert v-if="failedScopes.length" theme="error" role="alert">
        无法读取部分勘误文档：{{ failedScopes.map(kbName).join('、') }}。
        <t-button variant="text" size="small" :disabled="listBusy" @click="loadPages(failedScopes)">重试</t-button>
      </t-alert>
      <p v-if="historyUnavailable" role="status" class="metadata">当前范围没有可查看历史勘误的知识库。</p>
      <p v-if="listBusy" role="status">正在读取勘误文档…</p>
      <div v-if="rows.length" class="workspace-list-scroll">
        <div class="workspace-list-table" role="table" aria-label="勘误文档列表">
          <div class="workspace-list-head" role="row">
            <span role="columnheader">原文</span>
            <span role="columnheader" class="workspace-head-kb">知识库</span>
            <span role="columnheader" class="workspace-head-version">版本</span>
            <span role="columnheader" class="workspace-head-count">有效条数</span>
            <span role="columnheader" class="workspace-head-date">最近审核</span>
          </div>
          <div class="workspace-list-body" role="rowgroup">
            <div v-for="row in rows" :key="row.rowId" class="workspace-list-row correction-row" role="row">
              <div class="workspace-cell workspace-cell-main" role="cell">
                <div class="workbench-title-line">
                  <t-button :id="row.rowId" variant="text" size="small" class="workbench-row-title" @click="router.push(detailRoute(row))">{{ row.document_title }}</t-button>
                </div>
                <span class="workspace-compact-meta">
                  <span>{{ kbName(row.knowledge_base_id) }}</span>
                  <span>版本 {{ row.version_id }}</span>
                  <span>{{ row.active_count }} 条有效</span>
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
        <t-empty description="没有符合筛选条件的勘误文档。" />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DOMPurify from 'dompurify'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { correctionDocument, correctionDocuments, correctionMarkdown, type CorrectionDocument, type CorrectionDocumentSummary, type CorrectionFilters, type CorrectionItem, type CorrectionState } from '@/api/business'
import { authority } from '@/api/lifecycle'
import { renderDocumentPreviewMarkdown } from '@/utils/documentPreviewMarkdown'
import { requestFence } from '../lifecycle/model'
import { approvalBound, browseCorrectionState, correctionItemId, correctionRoute, correctionRowId, correctionStateLabels, markdownFileName } from './model'

const props = defineProps<{ scope: string; knowledgeBases: { id: string; name: string }[] }>()
const emit = defineEmits<{ open: [id: string, title: string, kb: string] }>()
const route = useRoute(), router = useRouter()
const routeText = (key: string) => typeof route.query[key] === 'string' ? route.query[key] : ''
const kbName = (kb: string) => props.knowledgeBases.find(row => row.id === kb)?.name || kb
const formatDate = (value: string) => value ? new Date(value).toLocaleString() : '—'
const scopeMode = ref<'current' | 'all'>('current')
const scopeOptions = computed(() => [
  { value: 'current', label: kbName(props.scope) }, { value: 'all', label: '全部有权限的知识库' },
])
const stateOptions = Object.entries(correctionStateLabels).map(([value, label]) => ({ value, label }))
const advancedOpen = ref(false)
const draft = reactive({ documentId: routeText('sourceDocument'), keyword: '', state: 'active' as CorrectionState, from: '', to: '' })
const filters = ref<CorrectionFilters>({ state: 'active', document_id: draft.documentId })
const listFence = requestFence(), detailFence = requestFence(), downloadFence = requestFence()
const listBusy = ref(false), filterError = ref(''), historyUnavailable = ref(false)
const pages = ref<Record<string, number>>({}), failedScopes = ref<string[]>([])
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
const selectedItem = computed(() => detail.value?.items.find(item => item.id === target.value?.item && String(item.revision) === target.value.revision))
const preview = computed(() => detail.value ? renderDocumentPreviewMarkdown(detail.value.markdown, html => DOMPurify.sanitize(html, {
  ALLOWED_TAGS: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'br', 'hr', 'ul', 'ol', 'li', 'blockquote', 'pre', 'code', 'strong', 'em', 'del', 'table', 'thead', 'tbody', 'tr', 'th', 'td'],
  ALLOWED_ATTR: [], ALLOW_ARIA_ATTR: false, ALLOW_DATA_ATTR: false,
})) : '')

function restartList() {
  listFence.invalidate()
  records.value = []
  pages.value = Object.fromEntries(scopes.value.map(kb => [kb, 1]))
  failedScopes.value = []
  historyUnavailable.value = false
  listBusy.value = false
  if (scopes.value.length) void loadPages(scopes.value)
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
    filterError.value = '审核日期起不能晚于审核日期止。'
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
    if (detailFence.current(token)) itemHeading.value?.focus()
    const canWrite = await permission
    if (detailFence.current(token)) canWriteDetail.value = canWrite
  } catch {
    if (detailFence.current(token)) detailError.value = '无法读取此版本的勘误文档。请核对知识库、版本和查看权限。'
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
    const url = URL.createObjectURL(new Blob([result.markdown], { type: 'text/markdown;charset=utf-8' }))
    const link = document.createElement('a')
    link.href = url
    link.download = markdownFileName(result.file_name)
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch {
    if (downloadFence.current(token)) {
      detail.value = undefined
      detailError.value = '无法重新核验勘误文档的下载权限，请重新载入。'
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
onBeforeUnmount(() => { listFence.invalidate(); detailFence.invalidate(); downloadFence.invalidate() })
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
