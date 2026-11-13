import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as model from './model'
import { requestFence } from '../lifecycle/model'

function mountSetup(api: Record<string, unknown>, routeQuery: Record<string, string> = {}) {
  const scope = vue.effectScope()
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const modules: Record<string, unknown> = {
    vue: { ...vue, onMounted() {}, onBeforeUnmount() {} },
    'vue-router': { useRoute:()=>({query:routeQuery}), useRouter:()=>({}), onBeforeRouteLeave(){}, onBeforeRouteUpdate(){} },
    '@/api/business': {},
    '@/api/knowledge-base': { listKnowledgeBases: async () => ({ success: true, data: [] }) },
    '@/api/lifecycle': {
      overview: async () => ({
        counts: {}, total: 0, page: 1, page_size: 20, documents: [],
        rules: { rules: { effective_delay_days: 0, valid_days: 0, review_days: 0 }, source: { tenant_id: 1, knowledge_base_id: 'kb' }, revision: 0 },
        rule_revision: 0, can_write: true,
      }),
      reviews: async () => ({ alerts: [], truncated: false }),
      ...api,
    },
    './SupplementEditor.vue': {},
    '@/stores/auth': { useAuthStore: () => ({ effectiveTenantId: 7 }) },
    './model': model,
  }
  const state = scope.run(() => new Function('require', 'defineProps', 'setInterval', 'clearInterval', 'window', 'document', 'exports', `${code}\nreturn {kb,tab,data,busy,error,reviewItems,reviewError,reviewBusy,reviewsLoaded,loadOverview,loadReviews,loadAll,loadKnowledgeBases,page,filter,titleQuery};`)(
    (id: string) => modules[id], () => ({}), () => 0, () => {}, {}, { activeElement: null, querySelector: () => null }, {},
  ))
  return { state, stop: () => scope.stop() }
}

test('late overview response from previous KB cannot replace current data', async () => {
  let releaseOld!: (value: unknown) => void
  const view = mountSetup({
    overview: async (kb: string) => {
      if (kb === 'kb-a') return new Promise(resolve => { releaseOld = resolve })
      return {
        counts: {}, total: 0, page: 1, page_size: 20, documents: [],
        rules: { rules: { effective_delay_days: 0, valid_days: 0, review_days: 0 }, source: { tenant_id: 1, knowledge_base_id: 'kb-b' }, revision: 0 },
        rule_revision: 0, can_write: false,
      }
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    assert.equal(typeof releaseOld, 'function')
    view.state.kb.value = 'kb-b'; await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.data.value?.can_write, false)
    releaseOld({
      counts: {}, total: 0, page: 1, page_size: 20, documents: [],
      rules: { rules: { effective_delay_days: 0, valid_days: 0, review_days: 0 }, source: { tenant_id: 1, knowledge_base_id: 'kb-a' }, revision: 0 },
      rule_revision: 0, can_write: true,
    })
    await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.data.value?.can_write, false)
  } finally { view.stop() }
})

test('late reviews response from previous KB cannot replace current reviews', async () => {
  let releaseOld!: (value: unknown) => void
  const view = mountSetup({
    reviews: async (kb: string) => {
      if (kb === 'kb-a') return new Promise(resolve => { releaseOld = resolve })
      return { alerts: [{ alert_id: 'new', document_id: 'new-doc', review_due: '2026-01-01T00:00:00Z', delivery_state: 'pending' }], truncated: false }
    },
  })
  try {
    view.state.kb.value = 'kb-a'; await vue.nextTick(); await vue.nextTick()
    view.state.tab.value = 'reviews'; await vue.nextTick(); await vue.nextTick()
    assert.equal(typeof releaseOld, 'function')
    view.state.kb.value = 'kb-b'; await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.reviewItems.value[0]?.alert_id, 'new')
    releaseOld({ alerts: [{ alert_id: 'old', document_id: 'old-doc', review_due: '2026-01-01T00:00:00Z', delivery_state: 'pending' }], truncated: false })
    await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.reviewItems.value[0]?.alert_id, 'new')
  } finally { view.stop() }
})

test('route query seeds the list tab, page, state and title', () => {
  const view = mountSetup({}, { tab: 'reviews', page: '3', state: 'active', title: '合同' })
  try {
    assert.equal(view.state.tab.value, 'reviews')
    assert.equal(view.state.page.value, 3)
    assert.equal(view.state.filter.value, 'active')
    assert.equal(view.state.titleQuery.value, '合同')
  } finally { view.stop() }
})
