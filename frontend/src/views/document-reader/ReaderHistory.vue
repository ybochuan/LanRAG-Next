<template>
  <div class="reader-history" :aria-busy="loading">
    <h3>{{ $t(mode === 'history' ? 'reader.groups.history' : 'workbench.lineage.modeProvenance') }}</h3>
    <t-radio-group v-if="mode === 'sources'" v-model="sourceMode" :aria-label="$t('workbench.lineage.viewTabsAria')">
      <t-radio-button value="provenance">{{ $t('workbench.lineage.modeProvenance') }}</t-radio-button>
      <t-radio-button value="renditions">{{ $t('workbench.lineage.modeRenditions') }}</t-radio-button>
    </t-radio-group>
    <t-loading v-if="loading" size="small" :text="$t('workbench.lineage.loading')" />
    <t-alert v-else-if="error" theme="warning">{{ $t(`reader.error.${error}`) }}
      <t-button variant="text" size="small" @click="retry++">{{ $t('workbench.common.retry') }}</t-button>
    </t-alert>
    <template v-else>
      <t-alert v-if="partial" theme="warning">
        {{ $t('reader.partialData') }}
        <span v-if="partialError">{{ $t(`reader.error.${partialError}`) }}</span>
        <t-button variant="text" size="small" @click="retry++">{{ $t('workbench.common.retry') }}</t-button>
      </t-alert>
      <template v-if="family">
        <h4 v-if="doc.version || doc.kind">{{ $t('reader.currentFamilyContext') }}</h4>
        <strong>{{ family.label }}</strong>
        <t-tag v-if="publishedTitle" theme="success" variant="light">{{ $t('reader.published', { title: publishedTitle }) }}</t-tag>
        <t-alert v-else-if="!family.published_document_id || family.published_head_availability === 'missing'" theme="info">{{ $t('reader.noPublished') }}</t-alert>
        <ol v-if="steps.length" class="reader-history-list" role="list">
          <li v-for="step in steps" :key="step.id">
            <p>{{ title(step.old_document_id) }} → {{ title(step.new_document_id) }}</p>
            <p>{{ $t('workbench.familyUi.effectiveAt', { date: formatDate(step.effective_at) }) }} · {{ replacementStatus(step.status) }}</p>
            <t-button v-if="step.status === 'applied' && readerID(step.old_document_id) && readerID(step.new_document_id)" size="small" variant="outline"
              @click="emit('compare', { kb: doc.kb, id: step.old_document_id }, { kb: doc.kb, id: step.new_document_id })">{{ $t('reader.compare') }}</t-button>
          </li>
        </ol>
        <t-alert v-if="moreReplacements" theme="info">{{ $t('reader.moreReplacementHistory') }}</t-alert>
      </template>
      <t-alert v-if="graphTruncated || graph?.truncated" theme="info">{{ $t('reader.truncatedData') }}</t-alert>
      <h4 v-if="nodes.length">{{ $t(mode === 'history' ? 'workbench.lineage.modeVersion' : 'workbench.lineage.nodeListAria') }}</h4>
      <ul v-if="nodes.length" class="reader-history-list" role="list">
        <li v-for="node in nodes" :key="node.id">
          <p><strong>{{ nodeName(node) }}</strong></p>
          <span v-if="node.created_at">{{ formatDate(node.created_at) }}</span>
          <t-tag v-if="node.current && node.type === 'document_version'" size="small" variant="light">{{ $t('workbench.lineage.currentObserved') }}</t-tag>
          <t-button v-if="node.type === 'document_version' && node.document_id" variant="text" size="small"
            @click="emit('open', { kb: doc.kb, id: node.document_id, version: node.id })">{{ $t('reader.versionRecord') }}</t-button>
        </li>
      </ul>
      <ul v-if="edges.length" class="reader-history-list" role="list">
        <li v-for="edge in edges" :key="edge.id">{{ edgeText(edge) }}</li>
      </ul>
      <t-empty v-if="!partial && !graphTruncated && !graph?.truncated && !nodes.length && !family" :description="$t('reader.noHistory')" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { documentDetail } from '@/api/lifecycle'
import { replacements, workspace, type BusinessFamily, type FamilyReplacement } from '@/api/families'
import { loadLineageGraph, type LineageEdge, type LineageGraph, type LineageNode } from '@/api/lineage'
import { useDocumentReaderStore } from '@/stores/documentReader'
import { statusLabel } from '../business/model'
import { edgeLabelKeys, typeLabelKeys } from '../lineage/model'
import { readerID, readerKey, type ReaderDocument } from './model'
import { ReaderError, readerError, readerMetadata, readerTitles } from './readerApi'

type Replacement = FamilyReplacement & { new_version_node_id?: string }
const props = defineProps<{ doc: ReaderDocument; mode: 'history' | 'sources' }>()
const emit = defineEmits<{ open: [doc: ReaderDocument]; compare: [left: ReaderDocument, right: ReaderDocument] }>()
const { t, locale } = useI18n()
const session = useDocumentReaderStore()
const sourceMode = ref<'provenance' | 'renditions'>('provenance')
const retry = ref(0)
const loading = ref(false)
const error = ref<ReturnType<typeof readerError>>()
const partialError = ref<ReturnType<typeof readerError>>()
const graphTruncated = ref(false)
const graph = shallowRef<LineageGraph>()
const family = shallowRef<BusinessFamily>()
const rawSteps = shallowRef<Replacement[]>([])
const moreReplacements = ref(false)
const partial = ref(false)
const titles = shallowRef(new Map<string, string>())
const title = (id: string) => titles.value.get(id) || t('reader.versionRecord')
const publishedID = computed(() => family.value?.published_head_availability !== 'missing' ? family.value?.published_document_id : undefined)
const publishedTitle = computed(() => publishedID.value ? titles.value.get(publishedID.value) : undefined)
const steps = computed(() => rawSteps.value.filter(step => titles.value.has(step.old_document_id) && titles.value.has(step.new_document_id)))
const nodes = computed(() => {
  const data = graph.value
  if (!data) return []
  const versions = data.nodes.filter(node => sameKB(node, props.doc.kb) && node.type === 'document_version' && titles.value.has(node.document_id))
  if (data.mode === 'provenance') return [...versions, ...data.nodes.filter(node => sameKB(node, props.doc.kb) && !!typeLabelKeys[node.type] && node.type !== 'document_version' && !node.document_id)]
  if (data.mode !== 'renditions') return versions
  const versionIDs = new Set(versions.map(node => node.id))
  const artifactIDs = new Set(data.edges.filter(edge => edge.type === 'rendition_of' && versionIDs.has(edge.to)).map(edge => edge.from))
  return [...versions, ...data.nodes.filter(node => sameKB(node, props.doc.kb) && node.type === 'parser_artifact' && artifactIDs.has(node.id))]
})
const nodeMap = computed(() => new Map(nodes.value.map(node => [node.id, node])))
const edges = computed(() => (graph.value?.edges || []).filter(edge => nodeMap.value.has(edge.from) && nodeMap.value.has(edge.to)))

function sameKB(node: LineageNode, kb: string) {
  return !node.knowledge_base_id || node.knowledge_base_id === kb
}

watch(() => [readerKey(props.doc), props.mode, sourceMode.value, session.scope, retry.value] as const, async (_, __, onCleanup) => {
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  const { signal } = controller
  const { doc, mode } = props
  const graphMode = mode === 'history' ? 'version-ancestry' : sourceMode.value
  graph.value = undefined
  graphTruncated.value = false
  family.value = undefined
  rawSteps.value = []
  moreReplacements.value = false
  titles.value = new Map()
  partial.value = false
  partialError.value = undefined
  error.value = undefined
  loading.value = true
  try {
    await readerMetadata(doc)
    signal.throwIfAborted()
    const [ancestry, governance] = await Promise.allSettled([
      loadVersionGraph(doc, graphMode, signal),
      mode === 'history' ? documentDetail(doc.kb, doc.id, true) : Promise.resolve(undefined),
    ])
    signal.throwIfAborted()
    if (ancestry.status === 'fulfilled') {
      graph.value = ancestry.value.graph
      graphTruncated.value = ancestry.value.rootLookupTruncated
    } else if (mode === 'sources' || governance.status === 'rejected') {
      throw ancestry.reason
    } else {
      setPartial(ancestry.reason)
    }
    if (governance.status === 'rejected') setPartial(governance.reason)
    const familyID = governance.status === 'fulfilled' ? governance.value?.business_family?.id : undefined
    if (familyID) await loadFamily(doc.kb, familyID, signal)
    signal.throwIfAborted()
    const ids = [publishedID.value, ...rawSteps.value.flatMap(step => [step.old_document_id, step.new_document_id]),
      ...(graph.value?.nodes || []).filter(node => sameKB(node, doc.kb) && node.type === 'document_version').map(node => node.document_id)]
      .filter((id): id is string => !!id)
    if (ids.length) {
      const authorized = await readerTitles(doc.kb, ids)
      signal.throwIfAborted()
      titles.value = authorized
      if (ids.some(id => !authorized.has(id))) partial.value = true
    }
  } catch (reason) { if (!signal.aborted) error.value = readerError(reason) }
  finally { if (!signal.aborted) loading.value = false }
}, { immediate: true, flush: 'sync' })

async function loadVersionGraph(doc: ReaderDocument, mode: 'version-ancestry' | 'provenance' | 'renditions', signal: AbortSignal) {
  let root = doc.version
  if (!root && mode !== 'renditions') {
    // Ancestry and provenance are rooted at observation nodes. The scoped
    // relations view accepts a native document ID and returns its observations.
    const index = await loadLineageGraph(doc.kb, 'relations', doc.id)
    signal.throwIfAborted()
    root = index.nodes.find(node => node.type === 'document_version' && sameKB(node, doc.kb) && node.document_id === doc.id && node.current)?.id
    if (!root) return { rootLookupTruncated: index.truncated }
  }
  const result = await loadLineageGraph(doc.kb, mode, root || doc.id)
  signal.throwIfAborted()
  if (root && (mode !== 'renditions' || result.nodes.length > 0) && !result.nodes.some(node =>
    node.id === root && node.type === 'document_version' && node.document_id === doc.id && sameKB(node, doc.kb) &&
    (doc.version || node.current))) throw new ReaderError('stale')
  return { graph: result, rootLookupTruncated: false }
}

async function loadFamily(kb: string, id: string, signal: AbortSignal) {
  const [info, history] = await Promise.allSettled([workspace(kb, id, 'members', 1), replacements(kb, id, 1)])
  signal.throwIfAborted()
  if (info.status === 'rejected') return setPartial(info.reason)
  if (info.value.family.business_family_id !== id) return setPartial(new ReaderError('stale'))
  family.value = info.value.family
  if (history.status === 'rejected') return setPartial(history.reason)
  if (history.value.some(step => step.business_family_id !== id)) return setPartial(new ReaderError('stale'))
  rawSteps.value = history.value.map((step: Replacement) => ({
    ...step, old_version_node_id: readerID(step.old_version_node_id) || '', new_version_node_id: readerID(step.new_version_node_id),
  }))
  if (history.value.length !== 20) return
  try {
    const next = await replacements(kb, id, 2)
    signal.throwIfAborted()
    if (next.some(step => step.business_family_id !== id)) throw new ReaderError('stale')
    moreReplacements.value = next.length > 0
  } catch (reason) { if (!signal.aborted) setPartial(reason) }
}

function setPartial(reason: unknown) {
  partial.value = true
  const code = readerError(reason)
  if (code === 'forbidden' || code === 'stale') partialError.value = code
}

function formatDate(value: string) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleString(locale.value) : ''
}
function replacementStatus(state: string) {
  return state === 'applied' ? t('workbench.familyUi.statusApplied') : statusLabel(state, t)
}
function nodeName(node: LineageNode) {
  const type = typeLabelKeys[node.type]
  return [node.document_id ? title(node.document_id) : '', type ? t(type) : '', node.sequence || ''].filter(Boolean).join(' · ')
}
function edgeText(edge: LineageEdge) {
  const source = nodeMap.value.get(edge.from)
  const target = nodeMap.value.get(edge.to)
  if (!source || !target) return ''
  const label = edgeLabelKeys[edge.type]
  return [nodeName(source), label ? t(label) : '', nodeName(target)].filter(Boolean).join(' → ')
}
</script>

<style scoped>
.reader-history { display: grid; gap: var(--td-comp-margin-m); overflow-wrap: anywhere; }
h3, h4, p { margin: 0; }
h3 { font-size: var(--td-font-size-title-medium); }
.reader-history-list { display: grid; gap: var(--td-comp-margin-m); padding-inline-start: var(--td-comp-paddingLR-l); margin: 0; }
</style>
