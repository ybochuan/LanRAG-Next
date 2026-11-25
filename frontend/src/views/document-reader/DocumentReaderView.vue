<template>
  <section ref="root" class="section reader-page" aria-labelledby="reader-title">
    <header class="section-header">
      <div class="section-header__top">
        <h2 id="reader-title">{{ $t('reader.title') }}</h2>
        <t-button v-if="canReturnToSource" variant="outline" @click="router.back()">{{ $t('reader.returnSource') }}</t-button>
        <t-button v-else variant="outline" @click="returnToWorkbench">{{ $t('reader.returnWorkbench') }}</t-button>
      </div>
      <p class="section-description">{{ $t('reader.subtitle') }}</p>
    </header>

    <t-alert v-if="!intent" theme="warning">{{ $t('reader.invalidLink') }}</t-alert>
    <t-card :bordered="true" class="reader-picker-card">
      <div class="reader-picker">
        <div class="reader-search" role="search" :aria-label="$t('reader.search')">
          <div class="reader-search__field reader-search__field--kb">
            <t-select v-model="pickerKB" :options="kbOptions" :loading="kbLoading" filterable
              :aria-label="$t('reader.knowledgeBase')" :placeholder="$t('reader.knowledgeBase')" />
          </div>
          <div class="reader-search__field reader-search__field--keyword">
            <t-input v-model="keyword" clearable :maxlength="160" :aria-label="$t('reader.search')"
              :placeholder="$t('reader.search')" :disabled="!pickerKB">
              <template #prefix-icon><t-icon name="search" size="16px" /></template>
            </t-input>
          </div>
          <div class="reader-search__field reader-search__field--document">
            <t-select v-model="selection" :options="fileOptions" :loading="filesLoading" clearable filterable
              :title="selectionLabel" :aria-label="$t('reader.choose')" :placeholder="$t('reader.choose')" :disabled="!pickerKB" />
          </div>
          <div class="reader-search__actions">
            <t-button v-if="hasMoreFiles && !filesError" variant="outline" :loading="filesLoading" @click="loadMoreFiles">{{ $t('common.loadMore') }}</t-button>
            <t-button class="reader-open-button" theme="primary" shape="square" :disabled="!selection"
              :aria-label="$t('reader.open')" :title="$t('reader.open')" @click="openSelection(false)">
              <t-icon name="browse" />
            </t-button>
            <t-button v-if="left" variant="outline" :disabled="!selection" @click="openSelection(true)">{{ $t('reader.compare') }}</t-button>
          </div>
        </div>
      </div>
    </t-card>
    <t-alert v-if="kbError || filesError" theme="warning">{{ $t('reader.pickerUnavailable') }}
      <t-button variant="text" size="small" @click="retryPicker">{{ $t('workbench.common.retry') }}</t-button>
    </t-alert>
    <div v-if="filesEmpty" role="status"><t-empty :description="$t('reader.pickerEmpty')" /></div>

    <nav v-if="session.tabs.length" class="reader-tabs" :aria-label="$t('reader.tabs')">
      <div v-for="tab in session.tabs" :key="readerKey(tab.doc)" class="reader-tab">
        <t-button size="small" :variant="left && readerKey(left) === readerKey(tab.doc) ? 'outline' : 'text'" :title="tab.title || $t('reader.original')"
          :aria-current="left && readerKey(left) === readerKey(tab.doc) ? 'page' : undefined" @click="open(tab.doc)">
          <span class="reader-tab-title">{{ tab.title || $t('reader.original') }}<template v-if="tab.doc.version"> · {{ tab.doc.version.slice(0, 8) }}</template></span>
        </t-button>
        <t-button variant="text" shape="square" size="small" :aria-label="$t('reader.closeTab', { title: tab.title || $t('reader.original') })" @click="closeTab(tab.doc)">
          <t-icon name="close" />
        </t-button>
      </div>
    </nav>

    <template v-if="left">
      <div class="reader-actions">
        <div class="reader-context-actions">
          <t-button v-for="item in panelActions" :key="item.id" size="small"
            :variant="panel === item.id ? 'outline' : 'text'" :theme="panel === item.id ? 'primary' : 'default'"
            :aria-expanded="panel === item.id" @click="togglePanel(item.id)">{{ $t(item.label) }}</t-button>
        </div>
        <div v-if="right" class="reader-compare-actions">
          <t-button v-if="!right.kind" size="small" variant="text" @click="swap">{{ $t('reader.swap') }}</t-button>
          <t-button size="small" variant="text" @click="open(left)">{{ $t('reader.exitCompare') }}</t-button>
          <t-radio-group v-model="activeSide" size="small" variant="primary-filled" :aria-label="$t('reader.pane')">
            <t-radio-button value="A">A</t-radio-button><t-radio-button value="B">B</t-radio-button>
          </t-radio-group>
        </div>
      </div>
      <div class="reader-layout" :class="{ 'reader-layout--relations': panel, 'reader-layout--narrow': narrow }">
        <t-card v-if="panel === 'history' || panel === 'sources'" :bordered="true" class="reader-relations">
          <ReaderHistory :doc="left" :mode="panel" @open="openHistoryRecord" @compare="open" />
        </t-card>
        <t-card v-else-if="panel === 'attachments'" :bordered="true" class="reader-relations">
          <ReaderAttachments :doc="left" :side="right ? 'A' : undefined" @open="open($event)" @compare="open(left, $event)" />
        </t-card>
        <t-card v-else-if="panel === 'relations'" :bordered="true" class="reader-relations">
          <h3>{{ $t('reader.relations') }}</h3>
          <t-alert v-if="left.version" theme="info">{{ $t('reader.relationStatus.outsideScope') }}</t-alert>
          <div v-else-if="relationStatus === 'loading'" role="status">
            <t-loading size="small" :text="$t('reader.loadingRelations')" />
          </div>
          <t-alert v-else-if="relationStatus !== 'ready' && relationStatus !== 'empty'" theme="warning">
            {{ $t(`reader.relationStatus.${relationStatus}`) }}
            <t-button variant="text" size="small" @click="relationRetry++">{{ $t('workbench.common.retry') }}</t-button>
          </t-alert>
          <t-empty v-else-if="relationStatus === 'empty'" :description="$t('reader.relationStatus.empty')" role="status" />
          <t-collapse v-if="groupedRelations.length" :default-value="relationGroups">
            <t-collapse-panel v-for="group in groupedRelations" :key="group.key" :value="group.key" :header="$t(`reader.groups.${group.key}`)">
              <ul class="reader-relation-list">
                <li v-for="item in group.items" :key="item.key">
                  <p>{{ item.label }}</p>
                  <t-tag size="small" theme="success" variant="light">{{ $t('reader.relationStatus.confirmed') }}</t-tag>
                  <t-button variant="text" size="small" :disabled="!!relationOpening" @click="openRelation(item, false)">{{ $t('reader.open') }}</t-button>
                  <t-button variant="text" size="small" :disabled="!!relationOpening" @click="openRelation(item, true)">{{ $t('reader.compare') }}</t-button>
                </li>
              </ul>
            </t-collapse-panel>
          </t-collapse>
        </t-card>
        <div ref="panes" class="reader-panes" :class="{ 'reader-panes--compare': right && !narrow }" :style="{ '--reader-split': split + 'fr', '--reader-rest': 100 - split + 'fr' }">
          <ReaderPane v-if="!right || !narrow || activeSide === 'A'" :doc="left" :side="right ? 'A' : undefined"
            :fullscreen-owner="!right || activeSide === 'A'" :refresh="refresh" source-mode @activate="activeSide = 'A'" @ready="onPaneReady">
            <template #actions="{ metadata }"><t-button v-if="metadata && canDownload(left.kb)" variant="text" size="small" :loading="downloading.includes(readerKey(left))" @click="download(left, metadata)">{{ $t('reader.download') }}</t-button></template>
          </ReaderPane>
          <PanelResizeHandle v-if="right && !narrow" edge="right" :label="$t('reader.split')"
            :value="split" :min="40" :max="60" @start="splitAtStart = split" @resize="resizeSplit" />
          <ReaderPane v-if="right && (!narrow || activeSide === 'B')" :doc="right" side="B"
            :fullscreen-owner="activeSide === 'B'" :refresh="refresh" source-mode @activate="activeSide = 'B'" @ready="onPaneReady">
            <template #actions="{ metadata }"><t-button v-if="metadata && !right.kind && canDownload(right.kb)" variant="text" size="small" :loading="downloading.includes(readerKey(right))" @click="download(right, metadata)">{{ $t('reader.download') }}</t-button></template>
          </ReaderPane>
        </div>
      </div>
    </template>
    <t-card v-else :bordered="true"><t-empty :description="$t('reader.empty')" /></t-card>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import PanelResizeHandle from '@/components/PanelResizeHandle.vue'
import { downKnowledgeDetails, listKnowledgeFiles } from '@/api/knowledge-base'
import { related } from '@/api/business'
import { useAuthStore } from '@/stores/auth'
import { useDocumentReaderStore } from '@/stores/documentReader'
import { saveBusinessDownload } from '@/utils/desktopDownload'
import { downloadResponseName, get } from '@/utils/request'
import { relationSentence } from '../business/model'
import ReaderPane from './ReaderPane.vue'
import ReaderHistory from './ReaderHistory.vue'
import ReaderAttachments from './ReaderAttachments.vue'
import { readerIntent, readerKey, readerRoute, relationGroup, relationGroups, type ReaderDocument } from './model'
import { ReaderError, readerError, readerMetadata, readerTitles, sameReaderFile, type NativeReaderDocument } from './readerApi'
import { useReaderPickerFiles } from './readerPicker'
import { relationIdentity, sameKBRelations, verifyRelationTargets, type VerifiedRelation } from './readerRelations'

interface KnowledgeBase { id: string; name: string; permission?: string; effective_permission?: string }
type PresentedRelation = VerifiedRelation & { label: string }
type RelationFailure = 'forbidden' | 'conflict' | 'unavailable' | 'stale'
const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const session = useDocumentReaderStore()
const root = ref<HTMLElement>()
const panes = ref<HTMLElement>()
const narrow = ref(false)
const activeSide = ref<'A' | 'B'>('A')
const split = ref(50)
let splitAtStart = split.value
const refresh = ref(0)
const intent = computed(() => readerIntent(route.query))
const left = computed(() => intent.value?.left)
const right = computed(() => intent.value?.right)

const knowledgeBases = ref<KnowledgeBase[]>([])
const kbLoading = ref(false)
const kbError = ref(false)
const pickerKB = ref('')
const keyword = ref('')
const kbReload = ref(0)
const kbOptions = computed(() => knowledgeBases.value.map(kb => ({ label: kb.name, value: kb.id })))
const panel = ref<'relations' | 'history' | 'sources' | 'attachments' | ''>('relations')
const panelActions = computed(() => ([
  { id: 'relations', label: 'reader.relations' },
  { id: 'history', label: 'reader.groups.history' },
  { id: 'sources', label: 'workbench.lineage.modeProvenance' },
  { id: 'attachments', label: 'reader.groups.attachments' },
] as const).filter(item => item.id !== 'attachments' || !left.value?.version))
const relationRetry = ref(0)
const relationStatus = ref<'loading' | 'ready' | 'empty' | 'partial' | RelationFailure>('unavailable')
const relationItems = ref<VerifiedRelation[]>([])
const relationSourceTitle = ref('')
const relationOpening = ref(false)
let relationRequest: AbortController | undefined
const downloading = ref<string[]>([])
const canReturnToSource = computed(() => {
  const back = router.options.history.state.back
  return !!route.fullPath && router.options.history.state.readerSource === true && typeof back === 'string' &&
    (back.startsWith('/platform/chat/') || back.startsWith('/platform/knowledge-bases/'))
})

let resize: ResizeObserver | undefined
let hiddenAt = 0
function revalidateOnReturn() {
  if (document.hidden) hiddenAt = Date.now()
  else if (hiddenAt && Date.now() - hiddenAt >= 30_000) {
    hiddenAt = 0
    refresh.value++
  }
}
onMounted(() => {
  document.addEventListener('visibilitychange', revalidateOnReturn)
  if (!root.value) return
  resize = new ResizeObserver(([entry]) => { narrow.value = entry.contentRect.width < 940 })
  resize.observe(root.value)
})
onBeforeUnmount(() => {
  resize?.disconnect()
  document.removeEventListener('visibilitychange', revalidateOnReturn)
})

watch([() => session.scope, () => auth.isLoggedIn, kbReload], async (_, __, onCleanup) => {
  const request = new AbortController()
  const { signal } = request
  onCleanup(() => { request.abort() })
  knowledgeBases.value = []
  pickerKB.value = ''
  kbLoading.value = false
  kbError.value = false
  if (!auth.isLoggedIn || !auth.effectiveTenantId) return

  kbLoading.value = true
  try {
    // The shared list cache is keyed by tenant. Resolve the current account's scope afresh.
    const response = await get<{ success?: boolean; data?: KnowledgeBase[] }>('/api/v1/knowledge-bases', { signal })
    if (signal.aborted) return
    if (!response.success || !Array.isArray(response.data)) throw new Error()
    knowledgeBases.value = response.data
    pickerKB.value = response.data.find(kb => kb.id === left.value?.kb)?.id ?? response.data[0]?.id ?? ''
  } catch { if (!signal.aborted) kbError.value = true }
  finally { if (!signal.aborted) kbLoading.value = false }
}, { immediate: true, flush: 'sync' })

const { selection, fileOptions, filesLoading, filesError, filesEmpty, hasMoreFiles, loadMoreFiles } =
  useReaderPickerFiles(pickerKB, keyword, () => session.scope, listKnowledgeFiles)
const selectionLabel = computed(() => fileOptions.value.find(option => option.value === selection.value)?.label || '')

watch(() => left.value && readerKey(left.value), () => {
  activeSide.value = 'A'
  if (left.value) session.visit(left.value)
}, { immediate: true, flush: 'sync' })

watch(() => intent.value?.view, view => { if (view) panel.value = view }, { immediate: true })
watch(() => !!right.value, (comparing, wasComparing) => {
  if (comparing && panel.value === 'relations') panel.value = ''
  else if (!comparing && wasComparing && !panel.value) panel.value = 'relations'
}, { immediate: true })

watch([
  () => left.value && readerKey(left.value), () => right.value && readerKey(right.value),
  panel, relationRetry, refresh, () => session.scope, () => auth.isLoggedIn,
], async (_, __, onCleanup) => {
  const doc = left.value
  const request = new AbortController()
  const { signal } = request
  onCleanup(() => { request.abort() })
  relationRequest = request
  relationItems.value = []
  relationSourceTitle.value = ''
  relationOpening.value = false
  relationStatus.value = 'unavailable'
  if (panel.value !== 'relations' || !doc || doc.version || !auth.isLoggedIn || !auth.effectiveTenantId) return

  relationStatus.value = 'loading'
  try {
    const source = await readerMetadata(doc)
    if (signal.aborted) return
    const rows = sameKBRelations(doc, await related(doc.kb, doc.id))
    if (signal.aborted) return
    const result = await verifyRelationTargets(doc.kb, rows, readerTitles, signal)
    if (signal.aborted) return
    relationSourceTitle.value = source.title || source.file_name || doc.id
    relationItems.value = result.items
    relationStatus.value = result.status
  } catch (error) {
    if (!signal.aborted) relationStatus.value = relationFailure(error)
  }
}, { immediate: true, flush: 'sync' })

const groupedRelations = computed(() => {
  const current = relationSourceTitle.value
  const items = relationItems.value.map((item): PresentedRelation => ({
    ...item,
    label: item.row.direction === 'incoming'
      ? relationSentence(item.row.edge_type, item.title, current, t)
      : relationSentence(item.row.edge_type, current, item.title, t),
  }))
  return relationGroups.map(key => ({ key, items: items.filter(item => relationGroup(item.row.edge_type) === key) })).filter(group => group.items.length)
})

function relationFailure(error: unknown, onOpen = false): RelationFailure {
  const code = readerError(error)
  if (code === 'conflict' || code === 'forbidden' || code === 'stale') return code
  return onOpen && code === 'missing' ? 'stale' : 'unavailable'
}
function retryPicker() {
  if (kbError.value) kbReload.value++
  else void loadMoreFiles()
}
function togglePanel(value: typeof panel.value) { panel.value = panel.value === value ? '' : value }
function replaceReader(location: ReturnType<typeof readerRoute> | { name: 'document-reader' }) {
  const { readerOrigin, readerSource } = router.options.history.state
  void router.replace({ ...location, state: { readerOrigin, readerSource } })
}
function open(doc: ReaderDocument, compare?: ReaderDocument) {
  replaceReader(readerRoute(doc, compare))
}
function openHistoryRecord(doc: ReaderDocument) {
  replaceReader(readerRoute(doc, undefined, panel.value === 'sources' ? 'sources' : 'history'))
}
function openSelection(compare: boolean) {
  if (!pickerKB.value || !fileOptions.value.some(option => option.value === selection.value)) return
  const doc = { kb: pickerKB.value, id: selection.value }
  if (!compare || !left.value) open(doc)
  else if (readerKey(doc) !== readerKey(left.value)) { open(left.value, doc); activeSide.value = 'B' }
}
function swap() { if (left.value && right.value) open(right.value, left.value) }
function closeTab(doc: ReaderDocument) {
  session.close(doc)
  if (!left.value || readerKey(left.value) !== readerKey(doc)) return
  const next = session.tabs.at(-1)
  if (next) open(next.doc)
  else replaceReader({ name: 'document-reader' })
}
function resizeSplit(delta: number) {
  const width = panes.value?.clientWidth
  if (width) split.value = Math.round(Math.min(60, Math.max(40, splitAtStart + delta / width * 100)))
}
function onPaneReady(doc: ReaderDocument, metadata: NativeReaderDocument) {
  const title = metadata.title || metadata.file_name || doc.id
  session.name(doc, title)
}
async function openRelation(item: PresentedRelation, compare: boolean) {
  const doc = left.value
  const signal = relationRequest?.signal
  if (!doc || doc.version || !signal || signal.aborted || relationOpening.value ||
    !relationItems.value.some(row => row.key === item.key)) return

  relationOpening.value = true
  try {
    const rows = sameKBRelations(doc, await related(doc.kb, doc.id))
    if (signal.aborted) return
    const row = rows.find(candidate => relationIdentity(candidate) === item.key)
    if (!row) throw new ReaderError('stale')
    const target = { kb: row.knowledge_base_id, id: row.document_id }
    await readerMetadata(target)
    if (signal.aborted) return
    if (compare) { open(doc, target); activeSide.value = 'B' }
    else open(target)
  } catch (error) {
    if (!signal.aborted) {
      relationItems.value = []
      relationSourceTitle.value = ''
      relationStatus.value = relationFailure(error, true)
    }
  } finally {
    if (!signal.aborted) relationOpening.value = false
  }
}
function returnToWorkbench() {
  const target = session.workbench
  if (target && router.options.history.state.readerOrigin === target.id) {
    if (router.options.history.state.back === target.path) router.back()
    else void router.push({ ...target.target, state: { readerReturn: target.id } })
  } else void router.push({ name: 'documents', query: { kb: left.value?.kb } })
}
function canDownload(kb: string) {
  const base = knowledgeBases.value.find(item => item.id === kb)
  const role = base?.effective_permission || base?.permission
  return !!base && auth.hasRole('contributor') && (!role || ['owner', 'admin', 'editor'].includes(role))
}
async function download(doc: ReaderDocument, metadata: NativeReaderDocument) {
  const key = readerKey(doc)
  const scope = session.scope
  if (doc.version || downloading.value.includes(key)) return
  downloading.value = [...downloading.value, key]
  try {
    if (!sameReaderFile(metadata, await readerMetadata(doc)) || scope !== session.scope) throw new ReaderError('changed')
    const blob = await downKnowledgeDetails(encodeURIComponent(doc.id))
    if (!sameReaderFile(metadata, await readerMetadata(doc)) || scope !== session.scope) throw new ReaderError('changed')
    const fileName = downloadResponseName(blob, metadata.file_name || metadata.title || doc.id)
    await saveBusinessDownload(blob, { fileName, sourceType: 'knowledge_document', sourceId: doc.id, sourceTitle: metadata.title || fileName, link: router.resolve(readerRoute(doc)).fullPath })
  } catch (error) { if (scope === session.scope) MessagePlugin.error(t(`reader.error.${readerError(error)}`)) }
  finally { downloading.value = downloading.value.filter(value => value !== key) }
}
</script>

<style scoped>
.reader-page { box-sizing: border-box; height: 100%; min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: var(--td-comp-margin-m); padding: 24px 32px; }
.section-header { display: flex; flex-direction: column; gap: var(--td-comp-margin-xxs); }
.section-header__top { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: var(--td-comp-margin-s); }
h2, h3, p { margin: 0; }
h2 { font-size: var(--td-font-size-title-large); }
h3 { font-size: var(--td-font-size-title-medium); }
.section-description { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-medium); }
.reader-picker-card { flex: 0 0 auto; }
.reader-picker-card > :deep(.t-card__body) { padding: var(--td-comp-paddingTB-m) var(--td-comp-paddingLR-l); }
.reader-picker { container-type: inline-size; display: flex; flex-direction: column; gap: var(--td-comp-margin-s); min-width: 0; }
.reader-search { display: grid; grid-template-columns: minmax(10rem, 14rem) minmax(12rem, 1fr) minmax(16rem, 1.4fr) auto; grid-template-areas: 'kb keyword document actions'; align-items: center; gap: var(--td-comp-margin-s); min-width: 0; }
.reader-search__field { min-width: 0; }
.reader-search__field--kb { grid-area: kb; }
.reader-search__field--keyword { grid-area: keyword; }
.reader-search__field--document { grid-area: document; }
.reader-search__field :deep(.t-select__wrap), .reader-search__field :deep(.t-input__wrap) { width: 100%; }
.reader-search__actions { grid-area: actions; display: flex; flex-wrap: nowrap; align-items: center; justify-content: flex-end; gap: var(--td-comp-margin-s); }
.reader-open-button { flex: 0 0 32px; width: 32px; min-width: 32px; padding: 0; }
.reader-open-button :deep(.t-icon) { margin: 0; }
.reader-search__actions :deep(.t-button) { margin-inline-start: 0; }
@container (max-width: 64rem) {
  .reader-search {
    grid-template-columns: minmax(10rem, 14rem) minmax(12rem, 1fr) auto;
    grid-template-areas:
      'kb keyword keyword'
      'document document actions';
  }
}
@container (max-width: 36rem) {
  .reader-search {
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: 'kb' 'keyword' 'document' 'actions';
  }
  .reader-search__actions { flex-wrap: wrap; justify-content: flex-start; }
}
.reader-tabs { display: flex; align-items: center; gap: 2px; min-width: 0; overflow-x: auto; overflow-y: hidden; border-bottom: 1px solid var(--td-component-stroke); }
.reader-tab { display: flex; align-items: center; min-width: 0; flex: 0 0 auto; }
.reader-tab-title { max-width: 32em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.reader-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--td-comp-margin-s); min-width: 0; }
.reader-context-actions, .reader-compare-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--td-comp-margin-xs); min-width: 0; }
.reader-compare-actions { margin-inline-start: auto; }
.reader-layout { flex: 1; min-height: 22rem; display: grid; grid-template: minmax(0, 1fr) / minmax(0, 1fr); gap: var(--td-comp-margin-s); }
.reader-layout--relations:not(.reader-layout--narrow) { grid-template-columns: minmax(18rem, 24rem) minmax(0, 1fr); }
.reader-layout--narrow.reader-layout--relations { min-height: 30rem; grid-template-rows: minmax(0, auto) minmax(0, 1fr); }
.reader-relations { min-width: 0; min-height: 0; overflow: auto; }
.reader-relations > :deep(.t-card__body) { display: grid; gap: var(--td-comp-margin-s); padding: var(--td-comp-paddingTB-m) var(--td-comp-paddingLR-m); }
.reader-layout--narrow .reader-relations { max-height: min(16rem, 38vh); }
.reader-relation-list { display: grid; gap: var(--td-comp-margin-m); list-style: none; padding: 0; margin: 0; overflow-wrap: anywhere; }
.reader-panes { display: grid; grid-template: minmax(0, 1fr) / minmax(0, 1fr); gap: var(--td-comp-margin-s); min-height: 0; }
.reader-panes--compare { position: relative; grid-template-columns: minmax(0, var(--reader-split)) minmax(0, var(--reader-rest)); }
.reader-panes--compare > :deep(.panel-resize-handle) { position: absolute; grid-column: 1; right: 0; }
@media (max-width: 760px) { .reader-page { padding: 16px 20px; } .reader-search__actions :deep(.t-button) { flex: 1 1 auto; } }
</style>
