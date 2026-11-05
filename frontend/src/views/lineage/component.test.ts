import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import { requestFence } from '../lifecycle/model'
import * as model from './model'

test('late graph response cannot restore the previous knowledge base', async () => {
  let release!: (value: unknown) => void
  const source = parse(readFileSync(new URL('./LineageView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const scope = vue.effectScope()
  const modules: Record<string, unknown> = {
    vue: { ...vue, onMounted() {}, onBeforeUnmount() {} },
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace: async () => {} }) },
    '@/stores/auth': { useAuthStore: () => ({ effectiveTenantId: 7 }) },
    '@/api/knowledge-base': {}, '@/api/lineage': {
      loadLineageGraph: (kb: string) => kb === 'old' ? new Promise(resolve => { release = resolve }) : Promise.resolve({ nodes: [], edges: [], mode: 'version-ancestry', root_id: '', newScope: true }),
    },
    '../lifecycle/model': { requestFence }, './model': model, './LineageCanvas.vue': {},
  }
  const state = scope.run(() => new Function('require', 'exports', `${code}\nreturn {kb,data,loadGraph};`)((id: string) => modules[id], {}))
  try {
    state.kb.value = 'old'
    await vue.nextTick()
    const old = state.loadGraph()
    state.kb.value = 'new'
    await vue.nextTick()
    await state.loadGraph()
    release({ nodes: [{ id: 'private-old' }], edges: [] })
    await old
    assert.equal(state.data.value.newScope, true)
    assert.equal(state.data.value.nodes.length, 0)
  } finally { scope.stop() }
})
