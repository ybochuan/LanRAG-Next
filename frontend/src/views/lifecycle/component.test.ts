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
  const state = scope.run(() => new Function('require', 'defineProps', 'setInterval', 'clearInterval', 'window', 'document', 'exports', `${code}\nreturn {kb,tab,data,busy,error,reviewItems,reviewError,reviewBusy,reviewsLoaded,loadOverview,loadReviews,loadAll,loadKnowledgeBases,filter,titleQuery};`)(
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

test('route query seeds the list tab, state and title', () => {
  const view = mountSetup({}, { tab: 'reviews', state: 'active', title: '合同' })
  try {
    assert.equal(view.state.tab.value, 'reviews')
    assert.equal(view.state.filter.value, 'active')
    assert.equal(view.state.titleQuery.value, '合同')
  } finally { view.stop() }
})
test('every workspace view has a top-level tab entry', () => {
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const views = ['families', 'associations', 'feedback-governance', 'revisions', 'lineage']
  const tabs = source.match(/const workspaceTabs=\[([\s\S]*?)\] as const/)?.[1] ?? ''
  assert.ok(tabs.length > 0, 'workspaceTabs must be declared')
  for (const view of views) {
    assert.ok(tabs.includes(`key:'${view}'`), `workspaceTabs must contain ${view}`)
  }
  const template = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.template?.content ?? ''
  assert.ok(
    template.includes('v-for="item in workspaceTabs"'),
    'the top tab bar must render the workspace tabs',
  )
  assert.ok(
    !template.includes('showFeedback'),
    'the removed showFeedback helper must not be referenced any more',
  )
})

test('the workbench no longer hosts a message centre; the native bell owns reminders', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const source = parse(file).descriptor.scriptSetup!.content
  const tabs = source.match(/const workspaceTabs=\[([\s\S]*?)\] as const/)?.[1] ?? ''
  assert.ok(!tabs.includes("key:'messages'"), 'the message centre tab must be removed')
  assert.ok(!source.includes('loadMessages'), 'the merged inbox loader must be removed')
  assert.ok(!source.includes("view==='messages'"), 'the router must not accept a messages view')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(!template.includes('<MessageCenter'), 'the workbench must not render a second inbox')
})


test('the review panel shows the notification delivery state', () => {
  const template = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.template?.content ?? ''
  assert.ok(template.includes('reviewDeliveryLabel(alert.delivery_state)'), 'the panel must name the delivery state')
  assert.ok(template.includes('reviewDeliveryClass(alert.delivery_state)'), 'the panel must tone the delivery badge')
  assert.ok(template.includes('alert.delivered_at'), 'the panel must show when the local callback accepted the reminder')
  assert.ok(!template.includes('待复核 →'), 'the fixed pill must not hide the real delivery state')
  assert.ok(template.includes('不代表人工已读'), 'the panel must not present a delivered reminder as human-read')
})
test('the workspace tab label set matches the router view contract', () => {
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const tabs = source.match(/const workspaceTabs=\[([\s\S]*?)\] as const/)?.[1] ?? ''
  for (const view of ['families', 'lineage', 'associations', 'feedback-governance', 'revisions']) {
    assert.ok(source.includes(`view==='${view}'`), `router must accept ${view}`)
    assert.ok(tabs.includes(`key:'${view}'`), `workspaceTabs must expose ${view}`)
  }
})

test('the feedback governance page exposes pending sample review', () => {
  const source = parse(readFileSync(new URL('../business/BusinessView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const template = parse(readFileSync(new URL('../business/BusinessView.vue', import.meta.url), 'utf8')).descriptor.template?.content ?? ''
  assert.ok(source.includes("feedback_sample_review"), 'the write action must be wired into the view')
  assert.ok(source.includes("sample_decision:sampleDecision.value"), 'the review decision must be sent in the payload')
  assert.ok(template.includes('sampleReviewPending(detail)'), 'only pending samples may show review controls')
  assert.ok(template.includes('value="failed"'), 'a failed review option must exist')
  assert.ok(template.includes('失败则隔离该反馈及其指导'), 'failed review must explain that it quarantines guidance')
})

test('permission-only views use the lightweight authority endpoint', () => {
  const business = parse(readFileSync(new URL('../business/BusinessView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const families = parse(readFileSync(new URL('../families/FamilyWorkspaceView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  assert.ok(business.includes('await authority(scope)'), 'business view must not load a full overview just for can_write')
  assert.ok(!business.includes('overview(scope,1'), 'business view must not call overview for authority')
  assert.ok(families.includes('authority(scope)'), 'family workspace must use the lightweight authority read')
  assert.ok(!families.includes('overview(scope,1'), 'family workspace must not call overview for authority')
})

test('cross-KB creation from the target side is blocked with a clear hint', () => {
  const src = parse(readFileSync(new URL('../business/CreateForm.vue', import.meta.url), 'utf8'))
  const template = src.descriptor.template?.content ?? ''
  const script = src.descriptor.scriptSetup!.content
  assert.ok(script.includes("targetSideCrossBlocked"), 'a guard must exist for the target-side entry')
  assert.ok(script.includes("targetSideCrossBlocked.value)return"), 'submit must refuse the blocked entry')
  assert.ok(template.includes('targetSideCrossBlocked ||'), 'the submit button must be disabled')
  assert.ok(template.includes('跨库关系请前往源文档所在知识库发起'), 'the hint must name where to create cross-KB relations')
  assert.ok(template.includes('此入口已禁用'), 'the hint must state the entry is disabled')
})

test('review list supports ascending and descending review_due order', () => {
  const src = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8'))
  const template = src.descriptor.template?.content ?? ''
  const script = src.descriptor.scriptSetup!.content
  assert.ok(script.includes("reviewSort=ref<'asc'|'desc'>('asc')"), 'a sort direction must be tracked')
  assert.ok(script.includes('sortedReviewItems'), 'the list must be sorted before rendering')
  assert.ok(template.includes('v-for="alert in sortedReviewItems"'), 'the review list must render the sorted rows')
  assert.ok(template.includes('reviewSort=\'asc\'') && template.includes('reviewSort=\'desc\''), 'both directions must be selectable')
})

test('document list uses WeKnora text colour and shows all documents in a scroll area', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const script = parse(file).descriptor.scriptSetup!.content
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(file.includes('.document-link:hover,.document-link:focus-visible {'), 'document names should only turn brand-coloured on hover')
  assert.ok(file.includes('color:var(--td-text-color-primary);'), 'document names default to WeKnora primary text colour')
  assert.ok(script.includes('allDocuments.value=rows'), 'all document pages must be accumulated')
  assert.ok(template.includes('v-for="doc in sortedDocuments"'), 'table renders the full sorted document set')
  assert.ok(!template.includes('第 {{ page }} 页'), 'pagination footer must be removed')
  assert.ok(file.includes('max-height:60vh;'), 'the list must be a bounded scroll area')
})
