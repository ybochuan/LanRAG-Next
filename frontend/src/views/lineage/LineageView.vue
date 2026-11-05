<template>
  <main class="lineage-page">
    <header class="lineage-heading">
      <div><span class="eyebrow">文档治理</span><h1>血缘图谱</h1><p>沿着真实关系，查看内容来源与版本演变。</p></div>
      <span class="readonly">只读图谱</span>
    </header>
    <section class="scope-bar">
      <label for="lineage-kb">知识库</label>
      <select id="lineage-kb" v-model="kb" :disabled="catalogLoading" @change="root = ''">
        <option value="">选择知识库</option><option v-for="item in knowledgeBases" :key="item.id" :value="item.id">{{ item.name }}</option>
      </select>
      <span class="scope-note">仅当前知识库</span>
      <button :disabled="busy || !kb" @click="loadGraph">刷新</button>
    </section>
    <div class="view-tabs" role="tablist" aria-label="血缘视图">
      <button v-for="item in modes" :key="item.key" role="tab" :aria-selected="mode === item.key" :class="{ selected: mode === item.key }" @click="switchMode(item.key)">{{ item.label }}</button>
    </div>
    <p class="view-description">{{ modes.find(item => item.key === mode)?.description }}</p>
    <button v-if="root" class="return-all" @click="root = ''">返回全库视图</button>
    <p v-if="error" class="notice error" role="alert">{{ error }} <button @click="knowledgeBases.length ? loadGraph() : loadCatalog()">重试</button></p>
    <div v-if="catalogLoading || busy" class="empty" role="status">正在读取血缘关系…</div>
    <div v-else-if="!kb" class="empty">选择一个有查看权限的知识库，即可浏览已登记的血缘。</div>
    <template v-else-if="data">
      <p v-if="data.truncated" class="notice" role="status">当前最多展示 256 个节点和 512 条边。可选中节点，展开它的溯源或版本历史。</p>
      <p v-if="data.titles_unavailable" class="notice" role="status">文档名称暂时无法读取，当前以文档编号展示。</p>
      <p v-if="layoutGraph(data).hasCycle" class="notice" role="status">存在循环关系，图中已保留原始方向。</p>
      <div class="graph-toolbar">
        <span>{{ data.nodes.length }} 个节点 · {{ data.edges.length }} 条关系</span>
        <div class="zoom-actions"><button :disabled="!data.nodes.length" aria-label="缩小图谱" @click="canvas?.zoom(-0.15)">−</button><button :disabled="!data.nodes.length" aria-label="放大图谱" @click="canvas?.zoom(0.15)">＋</button><button :disabled="!data.nodes.length" @click="canvas?.fit()">适应画布</button></div>
      </div>
      <div v-if="!data.nodes.length" class="empty">尚无已登记的{{ mode === 'provenance' ? '内容来源' : '版本' }}节点。文档在来源核验或修订发布后会出现在这里。</div>
      <div v-else class="graph-workspace">
        <aside class="node-list" aria-label="图中节点">
          <label for="lineage-search">查找节点</label><input id="lineage-search" v-model="search" type="search" placeholder="在当前图中搜索名称或编号" maxlength="160" />
          <p class="subtle">{{ matches.length }} 个匹配结果</p>
          <button v-for="node in matches" :key="node.id" :class="{ active: selectedID === node.id }" @click="selectNode(node.id)">
            <strong>{{ nodeTitle(node) }}</strong><span>{{ typeLabels[node.type] || node.type }}<template v-if="node.sequence"> · V{{ node.sequence }}</template></span><small>{{ stateLabels[node.state] || node.id.slice(0, 12) }}</small>
          </button>
          <p v-if="!matches.length" class="subtle">当前图中没有匹配的节点。</p>
        </aside>
        <div class="canvas-panel"><LineageCanvas ref="canvas" :data="data" :selected-id="selectedID" @select="selectNode" /></div>
      </div>
      <section v-if="selected" class="node-detail" aria-label="节点详情">
        <div class="detail-title"><div><span class="eyebrow">{{ typeLabels[selected.type] || selected.type }}</span><h2>{{ nodeTitle(selected) }}</h2></div><button @click="selectedID = ''">关闭详情</button></div>
        <dl><div><dt>节点编号</dt><dd>{{ selected.id }}</dd></div><div v-if="selected.document_id"><dt>文档编号</dt><dd>{{ selected.document_id }}</dd></div><div v-if="selected.sequence"><dt>观测版本</dt><dd>V{{ selected.sequence }}{{ selected.current ? ' · 当前观测版本' : '' }}</dd></div><div v-if="selected.state"><dt>文档当前状态</dt><dd>{{ stateLabels[selected.state] || selected.state }}</dd></div><div v-if="selected.family_id"><dt>观测版本族</dt><dd>{{ selected.family_id }}</dd></div><div><dt>登记时间</dt><dd>{{ formatDate(selected.created_at) }}</dd></div></dl>
        <div class="detail-actions"><button @click="expandNode('provenance')">内容溯源</button><button @click="expandNode('version-ancestry')">版本历史</button><RouterLink v-if="selected.document_id" :to="{ path: '/platform/lifecycle', query: { kb } }">查看库内生命周期</RouterLink></div>
      </section>
    </template>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { loadLineageGraph, type GraphMode, type LineageGraph } from '@/api/lineage'
import { requestFence } from '../lifecycle/model'
import { graphError, layoutGraph, modes, nodeTitle, stateLabels, typeLabels } from './model'
import LineageCanvas from './LineageCanvas.vue'

const route = useRoute(), router = useRouter(), auth = useAuthStore()
const knowledgeBases = ref<{ id: string; name: string }[]>([])
const kb = ref(''), mode = ref<GraphMode>('version-ancestry'), root = ref(''), search = ref(''), selectedID = ref('')
const data = ref<LineageGraph>(), busy = ref(false), catalogLoading = ref(true), error = ref('')
const canvas = ref<InstanceType<typeof LineageCanvas>>()
const graphFence = requestFence(), catalogFence = requestFence()
const selected = computed(() => data.value?.nodes.find(node => node.id === selectedID.value))
const matches = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return data.value?.nodes.filter(node => !query || [nodeTitle(node), node.id, node.document_id].some(value => value.toLocaleLowerCase().includes(query))) || []
})
function selectNode(id: string) { selectedID.value = id; if (id) canvas.value?.focus(id) }
function switchMode(value: GraphMode) { mode.value = value; root.value = '' }
function expandNode(value: GraphMode) { if (selectedID.value) { root.value = selectedID.value; mode.value = value } }
function formatDate(value: string) { const date = new Date(value); return Number.isFinite(date.getTime()) ? date.toLocaleString() : value }
async function loadGraph() {
  const token = graphFence.begin()
  data.value = undefined; selectedID.value = ''; search.value = ''; error.value = ''; busy.value = false
  if (!kb.value) return
  busy.value = true
  try {
    const result = await loadLineageGraph(kb.value, mode.value, root.value)
    if (graphFence.current(token)) data.value = result
  } catch (reason) { if (graphFence.current(token)) error.value = graphError(reason) }
  finally { if (graphFence.current(token)) busy.value = false }
}
async function loadCatalog() {
  const token = catalogFence.begin()
  graphFence.invalidate(); kb.value = ''; data.value = undefined; knowledgeBases.value = []; error.value = ''; catalogLoading.value = true
  try {
    const response = await listKnowledgeBases()
    if (!catalogFence.current(token)) return
    if (!response.success) throw new Error()
    knowledgeBases.value = response.data || []
    const requested = typeof route.query.kb === 'string' ? route.query.kb : ''
    kb.value = knowledgeBases.value.find(item => item.id === requested)?.id || knowledgeBases.value[0]?.id || ''
    mode.value = modes.find(item => item.key === route.query.mode)?.key || 'version-ancestry'
    root.value = typeof route.query.node === 'string' && mode.value !== 'relations' ? route.query.node : ''
  } catch { if (catalogFence.current(token)) error.value = '无法读取知识库，请检查登录状态后重试。' }
  finally { if (catalogFence.current(token)) catalogLoading.value = false }
}
watch([kb, mode, root], () => {
  void loadGraph()
  if (kb.value) void router.replace({ query: { kb: kb.value, mode: mode.value, ...(root.value ? { node: root.value } : {}) } })
})
watch(() => auth.effectiveTenantId, () => void loadCatalog())
watch(() => route.query, query => {
  if (typeof query.kb === 'string' && knowledgeBases.value.some(item => item.id === query.kb)) kb.value = query.kb
  mode.value = modes.find(item => item.key === query.mode)?.key || 'version-ancestry'
  root.value = typeof query.node === 'string' && mode.value !== 'relations' ? query.node : ''
})
onMounted(() => void loadCatalog())
onBeforeUnmount(() => { graphFence.invalidate(); catalogFence.invalidate() })
</script>

<style scoped>
.lineage-page { height: 100%; overflow: auto; padding: 32px; color: var(--td-text-color-primary); background: var(--td-bg-color-page); box-sizing: border-box; }
.lineage-heading, .scope-bar, .detail-title, .graph-toolbar, .detail-actions, .zoom-actions { display: flex; align-items: center; gap: 12px; }
.lineage-heading, .detail-title, .graph-toolbar { justify-content: space-between; }
.eyebrow, .scope-note, .subtle, .lineage-heading p, .view-description { color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
h1 { margin: 6px 0; font-size: var(--app-text-4xl); } h2 { margin: 6px 0; font-size: var(--app-text-2xl); }
.readonly { color: #39704b; border: 1px solid #cce2d4; border-radius: var(--app-radius-pill); padding: 6px 12px; font-size: var(--app-text-sm); white-space: nowrap; }
button, select, input { border: 1px solid var(--td-component-border); border-radius: var(--app-radius-sm); padding: 8px 12px; font: inherit; font-size: var(--app-text-md); background: var(--td-bg-color-container); color: inherit; }
button { cursor: pointer; } button:hover { border-color: #5b8fc9; } button:disabled { opacity: .5; cursor: default; }
button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.scope-bar { padding: 20px 0; flex-wrap: wrap; } .scope-bar select { min-width: 220px; } .scope-bar > button { margin-left: auto; }
.view-tabs { display: flex; gap: 20px; border-bottom: 1px solid var(--td-component-border); }
.view-tabs button { padding: 12px 0; border: 0; border-radius: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--td-text-color-secondary); }
.view-tabs button.selected { color: var(--td-brand-color); border-bottom-color: var(--td-brand-color); font-weight: 600; }
.view-description { margin: 14px 0 22px; } .notice { padding: 12px 16px; background: #fff8e7; color: #805f1c; border-radius: var(--app-radius-sm); font-size: var(--app-text-md); }
.return-all { margin-bottom: 14px; }
.notice.error { background: #fcefee; color: #ad3b35; } .empty { padding: 80px 24px; text-align: center; color: var(--td-text-color-secondary); }
.graph-toolbar { padding: 12px 16px; border: 1px solid var(--td-component-border); border-bottom: 0; border-radius: var(--app-radius-lg) var(--app-radius-lg) 0 0; background: var(--td-bg-color-container); font-size: var(--app-text-md); }
.zoom-actions { margin-left: auto; gap: 6px; } .zoom-actions button { padding: 5px 10px; }
.graph-workspace { display: grid; grid-template-columns: 248px minmax(0, 1fr); border: 1px solid var(--td-component-border); border-radius: 0 0 var(--app-radius-lg) var(--app-radius-lg); overflow: hidden; }
.node-list { padding: 16px 12px; max-height: 640px; overflow: auto; box-sizing: border-box; background: var(--td-bg-color-container); border-right: 1px solid var(--td-component-border); }
.node-list label { display: block; font-size: var(--app-text-md); margin-bottom: 8px; } .node-list input { width: 100%; box-sizing: border-box; padding: 9px 8px; }
.node-list > button { display: block; text-align: left; width: 100%; margin-top: 8px; padding: 12px; border-color: transparent; background: var(--td-bg-color-page); }
.node-list > button.active { border-color: var(--td-brand-color); } .node-list strong, .node-list span, .node-list small { display: block; overflow-wrap: anywhere; }
.node-list strong { font-size: var(--app-text-md); line-height: 1.5; } .node-list span, .node-list small { font-size: var(--app-text-xs); margin-top: 5px; color: var(--td-text-color-secondary); }
.canvas-panel { min-width: 0; overflow: hidden; } .node-detail { margin-top: 18px; border: 1px solid var(--td-component-border); border-radius: var(--app-radius-lg); padding: 20px; background: var(--td-bg-color-container); }
dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; margin: 20px 0; } dt { font-size: var(--app-text-sm); color: var(--td-text-color-secondary); } dd { margin: 6px 0 0; font-size: var(--app-text-md); overflow-wrap: anywhere; }
.detail-actions a { color: var(--td-brand-color); text-decoration: none; font-size: var(--app-text-md); }
@media (max-width: 800px) { .lineage-page { padding: 20px 12px; } .graph-workspace { grid-template-columns: 190px minmax(0, 1fr); } .node-list { max-height: 460px; } dl { grid-template-columns: 1fr; } .graph-toolbar { flex-wrap: wrap; } }
</style>
