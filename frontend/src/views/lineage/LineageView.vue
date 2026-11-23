<template>
  <section class="lineage-page" :class="{ 'embedded-lineage': scope }">
    <header v-if="!scope" class="lineage-heading">
      <div><h2>{{ documentId ? $t('workbench.lineage.titleDocument') : $t('workbench.lineage.titleRelations') }}</h2></div>
      <div class="heading-actions">
        <t-button v-if="associationReturn" variant="outline" size="small" @click="returnToAssociation">{{ $t('workbench.lineage.backAssociation') }}</t-button>
        <t-tag variant="light" size="small">{{ $t('workbench.lineage.readOnly') }}</t-tag>
      </div>
    </header>
    <section v-if="!scope" class="scope-bar">
      <label for="lineage-kb">{{ $t('workbench.lineage.knowledgeBase') }}</label>
      <t-select id="lineage-kb" v-model="kb" :options="kbOptions" :disabled="catalogLoading" class="kb-select" @change="root = ''" />
      <span class="scope-note">{{ $t('workbench.lineage.scopeNote') }}</span>
      <t-button variant="outline" :disabled="busy || !kb" :loading="busy" @click="loadGraph">{{ $t('workbench.lineage.refresh') }}</t-button>
    </section>
    <div v-if="!documentId" class="view-tabs" role="tablist" :aria-label="$t('workbench.lineage.viewTabsAria')" @keydown="moveTabFocus">
      <t-button v-for="item in modes" :key="item.key" variant="text" role="tab" :tabindex="mode===item.key?0:-1" :aria-selected="mode === item.key" @click="switchMode(item.key)">{{ $t(modeLabelKeys[item.key]) }}</t-button>
    </div>
    <p class="view-description">{{ modeDescription }}</p>
    <p v-if="mode==='relations'" class="graph-note">{{ $t('workbench.lineage.relationsNote') }}</p>
    <label v-if="mode==='relations'" class="cross-toggle"><t-checkbox v-model="includeCross">{{ $t('workbench.lineage.includeCross') }}</t-checkbox></label>
    <t-button v-if="documentId && mode!=='relations'" variant="text" class="return-all" @click="switchMode('relations')">{{ $t('workbench.lineage.backDocumentGraph') }}</t-button>
    <t-button v-else-if="root && !documentId" variant="text" class="return-all" @click="root = ''">{{ $t('workbench.lineage.backAll') }}</t-button>
    <t-alert v-if="error" theme="error">{{ error }} <t-button variant="text" size="small" @click="knowledgeBases.length ? loadGraph() : loadCatalog()">{{ $t('workbench.lineage.retry') }}</t-button></t-alert>
    <t-empty v-if="catalogLoading || busy" class="empty" role="status" :description="$t('workbench.lineage.loading')" />
    <t-empty v-else-if="!kb" class="empty" :description="$t('workbench.lineage.selectKb')" />
    <template v-else-if="data">
      <t-alert v-if="data.truncated" theme="info" role="status">{{ $t('workbench.lineage.truncated') }}</t-alert>
      <t-alert v-if="data.titles_unavailable" theme="info" role="status">{{ $t('workbench.lineage.titlesUnavailable') }}</t-alert>
      <t-alert v-if="layoutGraph(data).hasCycle" theme="info" role="status">{{ $t('workbench.lineage.cycle') }}</t-alert>
      <div class="graph-toolbar">
        <span>{{ $t('workbench.lineage.counts', { nodes: data.nodes.length, edges: data.edges.length }) }}</span>
        <div class="zoom-actions"><t-button variant="outline" size="small" :disabled="!data.nodes.length" :aria-label="$t('workbench.lineage.zoomOut')" @click="canvas?.zoom(-0.15)">−</t-button><t-button variant="outline" size="small" :disabled="!data.nodes.length" :aria-label="$t('workbench.lineage.zoomIn')" @click="canvas?.zoom(0.15)">＋</t-button><t-button variant="outline" size="small" :disabled="!data.nodes.length" @click="canvas?.fit()">{{ $t('workbench.lineage.fitCanvas') }}</t-button></div>
      </div>
      <div v-if="!data.nodes.length" class="empty"><template v-if="mode === 'renditions'">{{ $t('workbench.lineage.emptyRenditions') }}</template><template v-else>{{ $t('workbench.lineage.emptyNodes', { type: mode === 'provenance' ? $t('workbench.lineage.typeProvenance') : $t('workbench.lineage.typeVersion') }) }}</template></div>
      <div v-else class="graph-workspace">
        <aside class="node-list" :aria-label="$t('workbench.lineage.nodeListAria')">
          <label for="lineage-search">{{ $t('workbench.lineage.searchLabel') }}</label><t-input id="lineage-search" v-model="search" :placeholder="$t('workbench.lineage.searchPlaceholder')" :maxlength="160" clearable />
          <p class="subtle">{{ $t('workbench.lineage.matches', { n: matches.length }) }}</p>
          <t-button v-for="node in matches" :key="node.id" variant="text" class="node-button" :theme="selectedID===node.id?'primary':'default'" :aria-pressed="selectedID===node.id" @click="selectNode(node.id)">
            <strong>{{ nodeTitle(node, t) }}</strong><span>{{ typeText(node.type) }}<template v-if="node.sequence"> · V{{ node.sequence }}</template></span><small>{{ stateText(node.state) || node.id.slice(0, 12) }}</small>
          </t-button>
          <p v-if="!matches.length" class="subtle">{{ $t('workbench.lineage.noMatches') }}</p>
        </aside>
        <div class="canvas-panel"><LineageCanvas ref="canvas" :data="data" :selected-id="selectedID" @select="selectNode" /></div>
      </div>
      <section v-if="selected" class="node-detail" :aria-label="$t('workbench.lineage.nodeDetailAria')">
        <div class="detail-title"><div><span class="eyebrow">{{ typeText(selected.type) }}</span><h2>{{ nodeTitle(selected, t) }}</h2></div><t-button variant="text" @click="selectedID = ''">{{ $t('workbench.lineage.closeDetail') }}</t-button></div>
        <dl>
          <div><dt>{{ $t('workbench.lineage.nodeId') }}</dt><dd>{{ selected.id }}</dd></div>
          <div v-if="selected.knowledge_base_id"><dt>{{ $t('workbench.lineage.knowledgeBase') }}</dt><dd>{{ kbName(selected.knowledge_base_id) }}</dd></div>
          <div v-if="selected.document_id"><dt>{{ $t('workbench.lineage.documentId') }}</dt><dd>{{ selected.document_id }}</dd></div>
          <div v-if="selected.sequence"><dt>{{ $t('workbench.lineage.observedVersion') }}</dt><dd>V{{ selected.sequence }}{{ selected.current ? ' · ' + $t('workbench.lineage.currentObserved') : '' }}</dd></div>
          <div v-if="selected.state"><dt>{{ $t('workbench.lineage.documentState') }}</dt><dd>{{ stateText(selected.state) }}</dd></div>
          <div><dt>{{ $t('workbench.lineage.registeredAt') }}</dt><dd>{{ formatDate(selected.created_at) }}</dd></div>
        </dl>
        <div class="detail-actions"><t-button variant="outline" @click="expandNode('provenance')">{{ $t('workbench.lineage.provenance') }}</t-button><t-button variant="outline" @click="expandNode('version-ancestry')">{{ $t('workbench.lineage.history') }}</t-button>
          <t-button v-if="selected.type==='document_version' && selected.document_id" variant="outline" @click="$emit('correction',selected.knowledge_base_id || kb,selected.document_id,selected.id, !selected.current || (!!selected.state && selected.state!=='active'))">{{ $t('workbench.lineage.feedbackDocument') }}</t-button>
          <t-button v-if="selected.document_id" theme="primary" @click="$emit('open',selected.document_id,nodeTitle(selected, t),selected.knowledge_base_id)">{{ $t('workbench.lineage.openDocument') }}</t-button>
        </div>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { associationQueryKeys } from '../business/model'
import { moveTabFocus } from '../lifecycle/model'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { loadLineageGraph, type GraphMode, type LineageGraph } from '@/api/lineage'
import { requestFence } from '../lifecycle/model'
import { graphError, layoutGraph, modeDescriptionKeys, modeLabelKeys, modes, nodeTitle, typeLabelKeys, typeLabels } from './model'
import LineageCanvas from './LineageCanvas.vue'

const props = defineProps<{ scope?:string; documentId?:string }>()
defineEmits<{ open:[id:string,title:string,knowledgeBaseID?:string]; correction:[kb:string,documentId:string,versionId:string,history:boolean] }>()
const route = useRoute(), router=useRouter(), auth = useAuthStore()
const { t, te, locale } = useI18n()
const associationReturn=computed(()=>{
  const from=route.query.associationFrom
  if(props.documentId || typeof from!=='string' || !from)return undefined
  const context=Object.fromEntries(associationQueryKeys.map(key=>[key,typeof route.query[key]==='string'?route.query[key]:undefined]))
  return {name:'documents',query:{kb:from,view:'associations',...context}}
})
function returnToAssociation(){
  const to=associationReturn.value
  if(!to)return
  if(router.options.history.state.back===router.resolve(to).fullPath)router.back()
  else void router.push(to)
}
const knowledgeBases = ref<{ id: string; name: string }[]>([])
// TDesign select options for the KB scope picker.
const kbOptions = computed(() => [{ label: t('workbench.lineage.kbPlaceholder'), value: '' }, ...knowledgeBases.value.map(item => ({ label: item.name, value: item.id }))])
const kb = ref(''), mode = ref<GraphMode>('version-ancestry'), root = ref(''), search = ref(''), selectedID = ref('')
const includeCross = ref(false)
const data = ref<LineageGraph>(), busy = ref(false), catalogLoading = ref(true), error = ref('')
const canvas = ref<InstanceType<typeof LineageCanvas>>()
const graphFence = requestFence(), catalogFence = requestFence()
const selected = computed(() => data.value?.nodes.find(node => node.id === selectedID.value))
const graphMode = computed<GraphMode>(() => mode.value === 'relations' && includeCross.value ? 'relations-cross' : mode.value)
const modeDescription = computed(() => mode.value === 'relations' && includeCross.value
  ? t('workbench.lineage.crossDescription')
  : t(modeDescriptionKeys[mode.value]))
const matches = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return data.value?.nodes.filter(node => !query || [nodeTitle(node), node.id, node.document_id].some(value => value.toLocaleLowerCase().includes(query))) || []
})
function selectNode(id: string) { selectedID.value = id; if (id) canvas.value?.focus(id) }
function switchMode(value: GraphMode) { mode.value = value; includeCross.value = false; root.value = value==='relations' ? props.documentId || '' : '' }
function expandNode(value: GraphMode) { if (selectedID.value) { root.value = selectedID.value; mode.value = value; includeCross.value = false } }
function formatDate(value: string) { const date = new Date(value); return Number.isFinite(date.getTime()) ? date.toLocaleString(locale.value) : value }
function typeText(type: string) { return typeLabelKeys[type] ? t(typeLabelKeys[type]) : type }
function stateText(state?: string) { if (!state) return ''; const key = `workbench.states.${state}`; return te(key) ? t(key) : state }
function kbName(id: string) { return knowledgeBases.value.find(item => item.id === id)?.name || id }
async function loadGraph() {
  const token = graphFence.begin()
  data.value = undefined; selectedID.value = ''; search.value = ''; error.value = ''; busy.value = false
  if (!kb.value) return
  busy.value = true
  try {
    const scope=kb.value, requested=root.value, view=graphMode.value
    const result = await loadLineageGraph(scope, view, requested)
    if (!graphFence.current(token))return
    data.value=result
    if(!requested)return
    const rootNode=result.nodes.find(node=>node.id===requested)
      || result.nodes.find(node=>node.current && node.document_id===requested && (!node.knowledge_base_id || node.knowledge_base_id===scope))
    if(!rootNode)return
    busy.value=false
    await nextTick()
    if(graphFence.current(token))selectNode(rootNode.id)
  } catch (reason) { if (graphFence.current(token)) error.value = graphError(reason, t) }
  finally { if (graphFence.current(token)) busy.value = false }
}
async function loadCatalog() {
  const token = catalogFence.begin()
  graphFence.invalidate(); kb.value = props.scope || ''; data.value = undefined; knowledgeBases.value = []; error.value = ''; includeCross.value = false; catalogLoading.value = true
  if (props.scope) {
    mode.value = props.documentId ? 'relations' : modes.find(item => item.key === route.query.mode)?.key || 'version-ancestry'
    const requested = route.query.node || route.query.id
    root.value = props.documentId || (typeof requested==='string' ? requested : '')
  }
  try {
    const response = await listKnowledgeBases()
    if (!catalogFence.current(token)) return
    if (!response.success) throw new Error()
    knowledgeBases.value = response.data || []
    if (props.scope) return
    const requested = typeof route.query.kb === 'string' ? route.query.kb : ''
    kb.value = knowledgeBases.value.find(item => item.id === requested)?.id || knowledgeBases.value[0]?.id || ''
    mode.value = modes.find(item => item.key === route.query.mode)?.key || 'version-ancestry'
    root.value = typeof route.query.node === 'string' ? route.query.node : ''
  } catch { if (catalogFence.current(token)) error.value = t('workbench.lineage.loadKbError') }
  finally { if (catalogFence.current(token)) catalogLoading.value = false }
}
watch([kb, mode, root, includeCross], loadGraph)
watch(() => auth.effectiveTenantId, () => void loadCatalog())
watch(() => route.query, query => {
  if (props.scope) return
  if (typeof query.kb === 'string' && knowledgeBases.value.some(item => item.id === query.kb)) kb.value = query.kb
  mode.value = modes.find(item => item.key === query.mode)?.key || 'version-ancestry'
  root.value = typeof query.node === 'string' ? query.node : ''
})
onMounted(() => void loadCatalog())
onBeforeUnmount(() => { graphFence.invalidate(); catalogFence.invalidate() })
</script>

<style scoped>
.lineage-page.embedded-lineage { height:auto; overflow:visible; padding:0; }
.embedded-lineage .graph-workspace { grid-template-columns:minmax(145px, .35fr) minmax(0,1fr); }
.embedded-lineage :deep(.lineage-canvas) { height:420px; }
.lineage-page { height: 100%; overflow: auto; padding: 32px; color: var(--td-text-color-primary); background: transparent; box-sizing: border-box; }
.lineage-heading, .scope-bar, .detail-title, .graph-toolbar, .detail-actions, .zoom-actions { display: flex; align-items: center; gap: 12px; }
.lineage-heading, .detail-title, .graph-toolbar { justify-content: space-between; }
.eyebrow, .scope-note, .subtle, .lineage-heading p, .view-description { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-medium); }
h1 { margin: 6px 0; font-size: var(--td-font-size-title-large); } h2 { margin: 6px 0; font-size: var(--td-font-size-title-medium); }
.scope-bar { padding:12px 0; flex-wrap:wrap; }
.scope-bar > .t-button { margin-inline-start:auto; }
.view-tabs { display:flex; flex-wrap:wrap; gap:0 4px; border-bottom:1px solid var(--td-component-stroke); }
.view-tabs :deep([role="tab"]) { border-radius:0; border-bottom:2px solid transparent; }
.view-tabs :deep([aria-selected="true"]) { color:var(--td-brand-color); border-bottom-color:var(--td-brand-color); font-weight:600; }
.heading-actions { display:flex; align-items:center; flex-wrap:wrap; gap:8px; }
.graph-note { color:var(--td-text-color-secondary); font-size:var(--td-font-size-body-small); }
.view-description { margin:8px 0 12px; }
.cross-toggle { display:inline-flex; margin-bottom:12px; }
.notice,.return-all { margin-bottom:12px; }
.empty { padding:48px 24px; text-align:center; color:var(--td-text-color-secondary); }
.graph-toolbar { padding: 12px 16px; border: 1px solid var(--td-component-border); border-bottom: 0; border-radius: var(--td-radius-medium) var(--td-radius-medium) 0 0; background: var(--td-bg-color-container); font-size: var(--td-font-size-body-medium); }
.zoom-actions { margin-left: auto; gap: 6px; } .zoom-actions button { padding: 5px 10px; }
.graph-workspace { display: grid; grid-template-columns: 248px minmax(0, 1fr); border: 1px solid var(--td-component-border); border-radius: 0 0 var(--td-radius-medium) var(--td-radius-medium); overflow: hidden; }
.node-list { padding: 16px 12px; max-height: 640px; overflow: auto; box-sizing: border-box; background: var(--td-bg-color-container); border-right: 1px solid var(--td-component-border); }
.node-list label { display:block; margin-bottom:8px; }
.node-list .node-button { width:100%; margin-block:4px; justify-content:flex-start; text-align:left; height:auto; }
.node-button :deep(.t-button__text) { white-space:normal; overflow-wrap:anywhere; text-align:left; }
.node-button strong,.node-button span,.node-button small { display:block; }
.node-button span,.node-button small { color:var(--td-text-color-secondary); font-size:var(--td-font-size-body-small); }
.canvas-panel { min-width: 0; overflow: hidden; } .node-detail { margin-top: 18px; border: 1px solid var(--td-component-border); border-radius: var(--td-radius-large); padding: 20px; background: var(--td-bg-color-container); }
dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; margin: 20px 0; } dt { font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); } dd { margin: 6px 0 0; font-size: var(--td-font-size-body-medium); overflow-wrap: anywhere; }
.detail-actions a { color: var(--td-brand-color); text-decoration: none; font-size: var(--td-font-size-body-medium); }
@media (max-width:800px) {
  .lineage-page:not(.embedded-lineage) { padding:20px 16px; }
  dl { grid-template-columns:minmax(0,1fr); }
  .graph-toolbar,.detail-actions { flex-wrap:wrap; }
}
@media (max-width:600px) {
  .lineage-page .graph-workspace { grid-template-columns:minmax(0,1fr); }
  .node-list { max-height:176px; border-right:0; border-bottom:1px solid var(--td-component-stroke); }
  .embedded-lineage :deep(.lineage-canvas) { height:320px; }
}
</style>