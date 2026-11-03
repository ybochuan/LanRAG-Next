import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as model from './model'
import { requestFence } from '../lifecycle/model'

// Execute the actual setup script with controlled API completion and router hooks.
function mountSetup(api: Record<string, unknown>) {
  const guards: (() => boolean | void)[] = []
  const scope = vue.effectScope()
  const source = parse(readFileSync(new URL('./BusinessView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const modules: Record<string, unknown> = {
    vue: { ...vue, onMounted() {}, onBeforeUnmount() {} },
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({}), onBeforeRouteLeave: (guard: () => boolean) => guards.push(guard), onBeforeRouteUpdate: (guard: () => boolean) => guards.push(guard) },
    '@/stores/auth': { useAuthStore: () => ({ effectiveTenantId: 7 }) },
    '@/api/knowledge-base': { listKnowledgeBases: async () => ({ success: true, data: [] }) },
    '@/api/lifecycle': { overview: async () => ({ can_write: true, documents: [] }) },
    '@/api/business': { documentTitle: async () => '', ...api },
    './CreateForm.vue': {}, './DocumentPicker.vue': {}, '../lifecycle/model': { requestFence }, './model': model,
  }
  const state = scope.run(() => new Function('require', 'defineProps', 'setInterval', 'clearInterval', 'window', 'exports', `${code}\nreturn {kb,detail,pending,canWrite,perform,resetScope};`)(
    (id: string) => modules[id], () => ({ section: 'revisions' }), () => 0, () => {}, { addEventListener() {}, removeEventListener() {} }, {},
  ))
  return { state, guards, stop: () => scope.stop() }
}

test('unknown write keeps its original retry when navigating to another business view', async () => {
  const view = mountSetup({ list: async () => [] })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick()
    const pending = { kb: 'kb-a', intent: model.buildIntent('feedback_create', undefined, { topic: 'topic', text: 'content' }, 'original-operation') }
    view.state.pending.value = pending
    assert.equal(view.guards.length, 2)
    for (const guard of view.guards) assert.equal(guard(), false)
    assert.equal(view.state.pending.value.intent.body.operation_id, 'original-operation')
    view.state.pending.value = undefined
    for (const guard of view.guards) assert.equal(guard(), true)
  } finally { view.stop() }
})

test('completed write cannot replace new scope details after its slow list refresh', async () => {
  let release!: (rows: unknown[]) => void
  let defer = false
  const view = mountSetup({
    write: async () => ({ id: 'old-proposal', revision: 2, status: 'approved' }),
    list: async (kb: string, _kind: string, _page: number, id?: string) => {
      if (defer && kb === 'kb-a' && !id) return new Promise(resolve => { release = resolve })
      return []
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    view.state.canWrite.value = true
    view.state.pending.value = { kb: 'kb-a', intent: model.buildIntent('revision_approve', { id: 'old-proposal', revision: 1, status: 'pending_review' }, {}, 'approve') }
    defer = true
    const operation = view.state.perform(); await vue.nextTick()
    assert.equal(typeof release, 'function')
    view.state.kb.value = 'kb-b'; await vue.nextTick()
    view.state.detail.value = { id: 'new-proposal', revision: 4, status: 'pending_review' }
    release([]); await operation
    assert.equal(view.state.detail.value?.id, 'new-proposal')
  } finally { view.stop() }
})
