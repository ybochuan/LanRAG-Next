// Local browser fixture for the real Vue/X6 components. Production never imports it.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../../', import.meta.url))
const nodes = [
  { id: 'v3', title: '采购管理办法（2026 修订版）', sequence: 3, current: true, state: 'active', document_id: 'policy-2026' },
  { id: 'v2', title: '采购管理办法（2025 版）', sequence: 2, current: true, state: 'superseded', document_id: 'policy-2025' },
  { id: 'v1', title: '采购管理办法（2024 版）', sequence: 1, current: true, state: 'superseded', document_id: 'policy-2024' },
  { id: 'notice', title: '采购审批权限补充通知', sequence: 1, current: true, state: 'active', document_id: 'notice-doc' },
  { id: 'answer', title: '本轮答复的引用分块', sequence: 0, current: false, state: '', document_id: '', type: 'chunk' },
].map(node => ({ type: 'document_version', family_id: 'observed-' + node.id, created_at: '2026-11-05T10:00:00Z', ...node }))
const edges = [
  { id: 's1', from: 'v3', to: 'v2', type: 'supersedes' },
  { id: 's2', from: 'v2', to: 'v1', type: 'supersedes' },
  { id: 's3', from: 'v3', to: 'notice', type: 'merged_into' },
  { id: 's4', from: 'notice', to: 'v1', type: 'supersedes' },
  { id: 'd1', from: 'answer', to: 'v3', type: 'derived_from' },
  { id: 'r1', from: 'notice', to: 'v3', type: 'supplements' },
]
const modules = {
  '/lineage-entry.js': `import{createApp,h}from'vue';import{createRouter,createWebHistory,RouterView}from'vue-router';import'tdesign-vue-next/es/style/index.css';import'/src/assets/theme/theme.css';import Page from'/src/views/lineage/LineageView.vue';const router=createRouter({history:createWebHistory(),routes:[{path:'/:pathMatch(.*)*',component:Page}]});createApp({render:()=>h(RouterView)}).use(router).mount('#app')`,
  '/lineage-auth.js': 'export const useAuthStore=()=>({effectiveTenantId:7})',
  '/lineage-kb.js': "export const listKnowledgeBases=async()=>({success:true,data:[{id:'kb-a',name:'制度管理库'},{id:'kb-b',name:'空白示例库'}]})",
  '/lineage-request.js': "export const get=async path=>{const r=await fetch('/lineage-fixture'+path);if(!r.ok)throw{$httpStatus:r.status};return r.json()}",
}
const server = await createServer({
  root, configFile: false, plugins: [vue(), {
    name: 'lineage-component-fixture', enforce: 'pre',
    transform(code, id) {
      if (id.includes('/views/lineage/') || id.endsWith('/api/lineage.ts')) return code.replaceAll("'@/stores/auth'", "'/lineage-auth.js'").replaceAll("'@/api/knowledge-base'", "'/lineage-kb.js'").replaceAll("'@/utils/request'", "'/lineage-request.js'")
    },
    resolveId(id) { if (modules[id]) return '\0' + id },
    load(id) { if (id.startsWith('\0')) return modules[id.slice(1)] },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url, 'http://127.0.0.1')
        if (url.pathname.startsWith('/platform/')) {
          res.setHeader('Content-Type', 'text/html')
          res.end('<!DOCTYPE html><html lang="zh-CN"><meta charset="utf-8"><title>AgentRAG 血缘图验收</title><style>html,body,#app{margin:0;height:100%;font-family:Arial,"Microsoft YaHei",sans-serif}</style><div id="app"></div><script type="module" src="/lineage-entry.js"></script></html>')
          return
        }
        if (!url.pathname.startsWith('/lineage-fixture/')) { next(); return }
        const mode = url.searchParams.get('mode') || 'version-ancestry', rootID = url.searchParams.get('id') || ''
        const allowed = mode === 'provenance' ? ['derived_from'] : mode === 'relations' ? ['supplements', 'amends', 'interprets', 'related_to'] : ['supersedes', 'merged_into']
        let graphEdges = edges.filter(edge => allowed.includes(edge.type))
        let graphNodes = nodes.filter(node => mode === 'provenance' || node.type === 'document_version')
        if (url.pathname.includes('/kb-b/')) { graphNodes = []; graphEdges = [] }
        if (rootID) {
          const seen = new Set([rootID])
          for (let changed = true; changed;) {
            changed = false
            for (const edge of graphEdges) if (seen.has(edge.from) && !seen.has(edge.to)) { seen.add(edge.to); changed = true }
          }
          graphNodes = graphNodes.filter(node => seen.has(node.id))
          graphEdges = graphEdges.filter(edge => seen.has(edge.from) && seen.has(edge.to))
        }
        res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({ success: true, data: { mode, root_id: rootID, nodes: graphNodes, edges: graphEdges, truncated: false, titles_unavailable: false } }))
      })
    },
  }], resolve: { alias: { '@': root + '/src' } }, server: { host: '127.0.0.1', port: Number(process.env.LINEAGE_FIXTURE_PORT || 18993), strictPort: true, hmr: false },
})
await server.listen()
process.stdout.write(server.resolvedUrls.local[0] + 'platform/lineage\n')
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, async () => { await server.close(); process.exit(0) })
