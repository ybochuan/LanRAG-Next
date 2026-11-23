import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import { translate } from '../../test-utils/i18n'
import * as model from './model'
import { httpStatus } from '../business/model'

function mountSetup(api: Record<string, unknown>, routeQuery: Record<string, string> = {}) {
  const scope = vue.effectScope()
  const route = vue.reactive({ query: routeQuery })
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const modules: Record<string, unknown> = {
    vue: { ...vue, onMounted() {}, onBeforeUnmount() {} },
    'vue-router': { useRoute:()=>route, useRouter:()=>({}), onBeforeRouteLeave(){}, onBeforeRouteUpdate(){} },
    '@/api/business': {},
    '@/api/lineage': {},
    '../feedback/model': {},
    '../business/model': { httpStatus },
    '@/api/knowledge-base': { listKnowledgeBases: async () => ({ success: true, data: [{ id: 'kb', name: '测试库' }] }) },
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
    '@/api/attachmentWorkbench': {
      attachmentTasks: async () => ({
        documents: [{ parent_id: 'parent-doc', parent_title: '主件', parent_file_name: 'main.docx', count: 1, items: [{ id: 'annex-1', title: '中文名', native_title: 'annex-01.docx', file_name: 'annex-01.docx', display_name: '中文名', mapping_status: 'candidate', reference_title: '附件1', reference_location: '第一条', title_state: '', reasons: ['name_review', 'mapping_review'] }] }],
        total: 1,
        truncated: false,
      }),
    },
    '@/stores/auth': { useAuthStore: () => ({ effectiveTenantId: 7 }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => translate(key, params), locale: { value: 'zh-CN' } }) },
    './model': model,
  }
  const state = scope.run(() => new Function('require', 'defineProps', 'setInterval', 'clearInterval', 'window', 'document', 'exports', `${code}\nreturn {kb,tab,data,busy,error,reviewItems,reviewError,reviewBusy,reviewsLoaded,attachmentTaskList,attachmentTaskError,attachmentTaskDocuments,taskReasonLabel,collapsedTaskSections,isTaskSectionCollapsed,toggleTaskSection,openPendingAttachment,loadOverview,loadReviews,loadAll,loadKnowledgeBases,filter,titleQuery,openDocument,submitDocument,reloadDocument,applyDateConflictDraft,dateForm,dateConflictDraft,documentConflict,cleanupItems,loadCleanup,confirmCleanup,cleanupPage,cleanupTotal,cleanupFilter,cleanupActionCount,cleanupSelected,cleanupSelectionCount,cleanupSelectable,cleanupPageAllSelected,cleanupPageIndeterminate,toggleCleanupSelection,toggleCleanupPageSelection,clearCleanupSelection,cleanupTask,batchCleanup,allDocuments,loadMoreDocuments,documentPageError,nextDocumentPage,documentGroups,mainDocumentCount,attachmentCount,isDocumentExpanded,toggleDocument,attachmentMatchesQuery,ruleForm,ruleDirty,ruleOutdated,resetRuleForm,submitRules};`)(
    (id: string) => modules[id], () => ({}), () => 0, () => {}, {}, { activeElement: null, querySelector: () => null }, {},
  ))
  return { state, route, stop: () => scope.stop() }
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

test('review tab loads attachment tasks alongside document reminders', async () => {
  const view = mountSetup({
    reviews: async () => ({ alerts: [{ alert_id: 'due-1', document_id: 'doc-1', review_due: '2026-01-01T00:00:00Z', delivery_state: 'pending' }], truncated: false }),
  })
  try {
    view.state.kb.value = 'kb'; await vue.nextTick(); await vue.nextTick()
    view.state.tab.value = 'reviews'; await vue.nextTick(); await vue.nextTick()
    assert.equal(view.state.reviewItems.value[0]?.alert_id, 'due-1')
    assert.equal(view.state.attachmentTaskList.value?.total, 1)
    assert.equal(view.state.attachmentTaskDocuments.value[0]?.items[0]?.reasons.includes('mapping_review'), true)
    assert.equal(view.state.taskReasonLabel('name_review'), translate('workbench.reviews.attachment.reasonName'))
  } finally { view.stop() }
})

test('to-do sections collapse like native resource group headers', () => {
  const view = mountSetup({})
  try {
    assert.equal(view.state.isTaskSectionCollapsed('attachments'), false)
    assert.equal(view.state.isTaskSectionCollapsed('reviews'), false)
    view.state.toggleTaskSection('attachments')
    assert.equal(view.state.isTaskSectionCollapsed('attachments'), true)
    assert.equal(view.state.isTaskSectionCollapsed('reviews'), false)
    view.state.toggleTaskSection('attachments')
    assert.equal(view.state.isTaskSectionCollapsed('attachments'), false)
    view.state.toggleTaskSection('reviews')
    assert.equal(view.state.isTaskSectionCollapsed('reviews'), true)
  } finally { view.stop() }
  const template = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="task-section-header" role="button" tabindex="0"'), 'section headers must be native-style toggle rows')
  assert.ok(template.includes(':aria-expanded="!isTaskSectionCollapsed(\'attachments\')"'), 'the attachment section must expose its expanded state')
  assert.ok(template.includes(':aria-expanded="!isTaskSectionCollapsed(\'reviews\')"'), 'the review section must expose its expanded state')
  assert.ok(template.includes('aria-controls="attachment-task-body"') && template.includes('aria-controls="review-task-body"'), 'each toggle must point at its body')
  assert.ok(template.includes('class="task-section-toggle"') && template.includes("name=\"chevron-down\""), 'the toggle must use the native chevron affordance')
  assert.ok(template.includes(':inert="isTaskSectionCollapsed(\'attachments\')"'), 'collapsed content must leave the tab order')
})

test('route query seeds the list tab, state and title', () => {
  const view = mountSetup({}, { tab: 'reviews', state: 'active', title: '合同' })
  try {
    assert.equal(view.state.tab.value, 'reviews')
    assert.equal(view.state.filter.value, 'active')
    assert.equal(view.state.titleQuery.value, '合同')
  } finally { view.stop() }
})

test('same-page navigation and history restore the list tab and filters', async () => {
  const cleanupLoads: string[] = []
  const view = mountSetup({ cleanupList: async (kb: string) => { cleanupLoads.push(kb); return { tasks: [] } } }, { kb: 'kb', state: 'active', title: '合同' })
  try {
    await view.state.loadKnowledgeBases()
    await vue.nextTick()
    const initial = view.route.query
    view.route.query = { kb: 'kb', tab: 'cleanup' }
    await vue.nextTick()
    assert.equal(view.state.tab.value, 'cleanup')
    assert.equal(view.state.filter.value, '')
    assert.equal(view.state.titleQuery.value, '')
    assert.deepEqual(cleanupLoads, ['kb'])
    view.route.query = initial
    await vue.nextTick()
    assert.equal(view.state.tab.value, 'documents')
    assert.equal(view.state.filter.value, 'active')
    assert.equal(view.state.titleQuery.value, '合同')
  } finally { view.stop() }
})

test('cleanup uses the server filter, effective row status and scope-wide actionable count', async () => {
  const due = { document_id: 'due', status: 'pending_cleanup', effective_status: 'awaiting_confirmation', can_confirm: true }
  const requests: Array<[number, string]> = []
  const view = mountSetup({
    cleanupList: async (_kb: string, page: number, _pageSize: number, state: string) => {
      requests.push([page, state])
      return { tasks: [due], total: 1, actionable: 3 }
    },
  }, { kb: 'kb', tab: 'cleanup' })
  try {
    await view.state.loadKnowledgeBases()
    await vue.nextTick()
    view.state.cleanupFilter.value = 'awaiting_confirmation'
    requests.length = 0
    await view.state.loadCleanup(1)
    assert.deepEqual(requests, [[1, 'awaiting_confirmation']])
    assert.deepEqual(view.state.cleanupItems.value, [due])
    assert.equal(view.state.cleanupTotal.value, 1)
    assert.equal(view.state.cleanupActionCount.value, 3)
  } finally {
    view.stop()
  }
})

test('a failed cleanup refreshes the task so a retry uses the latest revision', async () => {
  let task = { knowledge_base_id: 'kb', document_id: 'doc', revision: 4, status: 'awaiting_confirmation', can_confirm: true }
  const revisions: number[] = []
  const view = mountSetup({
    cleanupList: async () => ({ tasks: [task] }),
    confirmCleanup: async (_kb: string, _id: string, revision: number) => {
      revisions.push(revision)
      if (revision === 4) {
        task = { ...task, revision: 5, status: 'failed' }
        throw { $httpStatus: 503 }
      }
    },
  })
  try {
    const state = view.state
    await state.loadKnowledgeBases()
    await vue.nextTick()
    await state.loadCleanup()
    await state.confirmCleanup(state.cleanupItems.value[0])
    assert.equal(state.cleanupItems.value[0]?.status, 'failed')
    await state.confirmCleanup(state.cleanupItems.value[0])
    assert.deepEqual(revisions, [4, 5])
  } finally { view.stop() }
})

test('cleanup selects only actionable records and cleans early records immediately', async () => {
  const calls: Array<[string, string, number]> = []
  let tasks = [
    { knowledge_base_id: 'kb', document_id: 'due', revision: 1, status: 'awaiting_confirmation', can_confirm: true, can_cleanup_now: true },
    { knowledge_base_id: 'kb', document_id: 'early', revision: 2, status: 'pending_cleanup', can_confirm: false, can_cleanup_now: true },
    { knowledge_base_id: 'kb', document_id: 'blocked', revision: 3, status: 'pending_cleanup', can_confirm: false, can_cleanup_now: false },
  ]
  const view = mountSetup({
    cleanupList: async () => ({ tasks, total: tasks.length }),
    confirmCleanup: async (_kb: string, id: string, revision: number) => {
      calls.push(['confirm', id, revision])
      tasks = tasks.filter(task => task.document_id !== id)
    },
    confirmCleanupNow: async (_kb: string, id: string, revision: number) => {
      calls.push(['now', id, revision])
      tasks = tasks.filter(task => task.document_id !== id)
    },
  }, { kb: 'kb', tab: 'cleanup' })
  try {
    const state = view.state
    await state.loadKnowledgeBases()
    await vue.nextTick()
    await state.loadCleanup()
    assert.equal(state.cleanupSelectable(tasks[0]), true)
    assert.equal(state.cleanupSelectable(tasks[1]), true)
    assert.equal(state.cleanupSelectable(tasks[2]), false)
    state.toggleCleanupSelection(tasks[2])
    assert.equal(state.cleanupSelectionCount.value, 0)
    state.toggleCleanupPageSelection(true)
    assert.equal(state.cleanupSelectionCount.value, 2)
    assert.equal(state.cleanupPageAllSelected.value, true)
    state.clearCleanupSelection()
    state.toggleCleanupSelection(tasks[1])
    await state.batchCleanup('cleanup')
    assert.deepEqual(calls, [['now', 'early', 2]])
  } finally { view.stop() }
})

test('the first document page renders immediately and a failed next page can be retried', async () => {
  const pages: number[] = []
  let fail = true
  const row = (document_id: string) => ({ document_id, annotations: {} })
  const fixture = mountSetup({ overview: async (_kb: string, page: number) => {
    pages.push(page)
    if (page === 2 && fail) { fail = false; throw new Error('fixture unavailable') }
    const documents = page === 1 ? Array.from({ length: 50 }, (_, index) => row(String(index))) : [row('last')]
    return { total: 51, page_size: 50, documents, rules: { rules: {} }, can_write: true }
  } })
  try {
    const { state } = fixture
    await state.loadKnowledgeBases()
    await vue.nextTick()
    assert.deepEqual(pages, [1])
    assert.equal(state.allDocuments.value.length, 50)
    await state.loadMoreDocuments()
    assert.equal(state.allDocuments.value.length, 50)
    assert.ok(state.documentPageError.value)
    await state.loadMoreDocuments()
    assert.deepEqual(pages, [1, 2, 2])
    assert.equal(state.allDocuments.value.at(-1)?.document_id, 'last')
    assert.equal(state.nextDocumentPage.value, 0)
  } finally { fixture.stop() }
})

test('cleanup reaches the 101st task and returns from an emptied last page', async () => {
  const pages: number[] = []
  let total = 101
  const doc = { knowledge_base_id: 'kb', document_id: 'last', revision: 1, can_confirm: true }
  const fixture = mountSetup({
    cleanupList: async (_kb: string, page: number) => {
      pages.push(page)
      return { total, tasks: page === 3 && total === 101 ? [doc] : [] }
    },
    confirmCleanup: async () => { total = 100 },
  })
  try {
    const { state } = fixture
    await state.loadKnowledgeBases()
    await vue.nextTick()
    await state.loadCleanup(3)
    assert.equal(state.cleanupItems.value[0]?.document_id, 'last')
    await state.confirmCleanup(doc)
    assert.deepEqual(pages, [3, 3, 2])
    assert.equal(state.cleanupPage.value, 2)
    assert.equal(state.cleanupTotal.value, 100)
  } finally { fixture.stop() }
})

test('refresh preserves the rules draft and blocks a stale save until reset', async () => {
  let revision = 1
  const writes: number[] = []
  const rules = () => ({ rules: { effective_delay_days: 0, valid_days: revision * 10, review_days: 0 }, token: String(revision) })
  const fixture = mountSetup({
    overview: async () => ({ total: 0, documents: [], rules: rules(), rule_revision: revision, can_write: true }),
    saveRules: async (_kb: string, expected: number) => { writes.push(expected); return { rules: rules(), rule_revision: revision } },
  })
  try {
    const { state } = fixture
    await state.loadKnowledgeBases()
    await vue.nextTick()
    state.ruleForm.valid_days = 45
    await state.loadOverview()
    assert.equal(state.ruleForm.valid_days, 45)
    assert.equal(state.ruleDirty.value, true)
    revision = 2
    await state.loadOverview()
    assert.equal(state.ruleForm.valid_days, 45)
    assert.equal(state.ruleOutdated.value, true)
    await state.submitRules()
    assert.deepEqual(writes, [])
    state.resetRuleForm()
    assert.equal(state.ruleForm.valid_days, 20)
    state.ruleForm.valid_days = 25
    await state.submitRules()
    assert.deepEqual(writes, [2])
  } finally { fixture.stop() }
})

test('date conflict reload uses a fresh revision and reapplies only the edited dates', async () => {
  const detailLoads: boolean[] = []
  const newUntil = '2027-05-01T09:00:00Z'
  const initial = { revision: 1, labels: {}, effective_until: '2027-04-01T09:00:00Z' }
  const current = { ...initial, revision: 2, effective_until: newUntil }
  const detail = (annotations: typeof initial) => ({ snapshot: { annotations }, can_write: true })
  const revisions: number[] = []
  const view = mountSetup({
    documentDetail: async (_kb: string, _id: string, fresh: boolean) => {
      detailLoads.push(fresh)
      return detail(fresh ? current : initial)
    },
    saveAnnotations: async (_kb: string, _id: string, revision: number, annotations: typeof initial & { review_due: string }) => {
      revisions.push(revision)
      if (revision === 1) throw { $httpStatus: 409 }
      assert.equal(annotations.effective_until, newUntil)
      assert.equal(model.toLocalInput(annotations.review_due), '2027-01-05T09:30')
      return detail({ ...annotations, revision: 3 })
    },
  })
  try {
    const state = view.state
    state.kb.value = 'kb'
    await vue.nextTick()
    await state.openDocument('doc', '合同')
    state.dateForm.review_due = '2027-01-05T09:30'
    await state.submitDocument()
    assert.equal(state.documentConflict.value, true)
    await state.reloadDocument()
    assert.deepEqual(detailLoads, [false, true])
    assert.equal(state.dateForm.effective_until, model.toLocalInput(newUntil))
    assert.equal(state.dateForm.review_due, '')
    assert.deepEqual(state.dateConflictDraft.value, { review_due: '2027-01-05T09:30' })
    await state.submitDocument()
    assert.deepEqual(revisions, [1])
    state.applyDateConflictDraft()
    await state.submitDocument()
    assert.deepEqual(revisions, [1, 2])
  } finally { view.stop() }
})
test('every workspace view has a top-level tab entry', () => {
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const views = ['families', 'associations', 'feedback-materials', 'revisions', 'lineage']
  const tabs = source.match(/const workspaceTabs=\[([\s\S]*?)\] as const/)?.[1] ?? ''
  assert.ok(tabs.length > 0, 'workspaceTabs must be declared')
  for (const view of views) {
    assert.ok(tabs.includes(`key:'${view}'`), `workspaceTabs must contain ${view}`)
  }
  const template = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.template?.content ?? ''
  assert.ok(
    template.includes('v-for="item in workspaceTabs.filter'),
    'the top tab bar must render the workspace tabs',
  )
  assert.ok(
    template.includes('panel-correction-documents') && template.includes('panel-feedback-review'),
    'the unified workspace must expose corrections and review as distinct panels',
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
  assert.ok(template.includes('reviewDeliveryLabel(alert.delivery_state, t)'), 'the panel must name the delivery state')
  assert.ok(template.includes('<t-tag'), 'the delivery badge must be a TDesign tag')
  assert.ok(template.includes(":theme=\"alert.delivery_state==='delivered'?'success':'default'\""), 'the delivery tag must be toned by delivery state')
  assert.ok(template.includes('alert.delivered_at'), 'the panel must show when the local callback accepted the reminder')
  assert.ok(!template.includes('待复核 →'), 'the fixed pill must not hide the real delivery state')
  assert.ok(template.includes("workbench.reviews.note"), 'the panel must not present a delivered reminder as human-read')
})
test('the workspace tab label set matches the router view contract', () => {
  const source = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const tabs = source.match(/const workspaceTabs=\[([\s\S]*?)\] as const/)?.[1] ?? ''
  for (const view of ['families', 'lineage', 'associations', 'feedback-materials', 'revisions']) {
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
  assert.ok(template.includes('<t-select v-model="sampleDecision"'), 'the review decision must be a TDesign select')
  assert.ok(source.includes('workbench.businessView.failQuarantineOption'), 'a failed review option must exist')
  assert.ok(template.includes("$t('workbench.businessView.sampleReviewNote')"), 'failed review must explain that it quarantines guidance')
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
  assert.match(script, /if\([^\n]*targetSideCrossBlocked\.value[^\n]*\)return/, 'submit must refuse the blocked entry')
  assert.ok(template.includes('targetSideCrossBlocked ||'), 'the submit button must be disabled')
  assert.ok(script.includes("fixedDocument?.end==='target'&&!readingRelation(form.edge_type)"), 'only same-library reading relations are offered with a fixed target')
  assert.ok(template.includes("$t('workbench.formExtra.targetBlocked')"), 'the hint must name where to create other relations')
  assert.match(translate('workbench.formExtra.targetBlocked'), /从正文详情可新增同库补充说明和内容提示/)
})

test('review list supports ascending and descending review_due order', () => {
  const src = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8'))
  const template = src.descriptor.template?.content ?? ''
  const script = src.descriptor.scriptSetup!.content
  assert.ok(script.includes("reviewSort=ref<'asc'|'desc'>('asc')"), 'a sort direction must be tracked')
  assert.ok(script.includes('sortedReviewItems'), 'the list must be sorted before rendering')
  assert.ok(template.includes('v-for="alert in sortedReviewItems"'), 'the review list must render the sorted rows')
  assert.ok(template.includes('<t-radio-group v-model="reviewSort"'), 'the sort switch must be a TDesign radio group')
  assert.ok(template.includes('value="asc"') && template.includes('value="desc"'), 'both directions must be selectable')
})

test('document list uses WeKnora text colour and shows all documents in a scroll area', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const script = parse(file).descriptor.scriptSetup!.content
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(file.includes('.document-link:hover,'), 'document names should only turn brand-coloured on hover')
  assert.ok(file.includes('background:transparent'), 'document names must not paint a hover plate')
  assert.ok(file.includes('.document-link :deep(.t-button__text) {'), 'the TDesign button text must wrap instead of being clipped')
  assert.ok(file.includes('white-space:normal;'), 'document names must wrap onto multiple lines')
  assert.ok(file.includes('color:var(--td-text-color-primary);'), 'document names default to WeKnora primary text colour')
  assert.ok(script.includes('loadMoreDocuments'), 'document pages load on demand')
  assert.ok(template.includes('v-for="group in documentGroups"'), 'table renders parent documents with their attachments')
  assert.ok(template.includes('<t-select id="lifecycle-state-filter"'), 'the state filter must be a TDesign select')
  assert.ok(template.includes('<t-input id="lifecycle-title-search"'), 'the title search must be a TDesign input')
  assert.ok(!template.includes('第 {{ page }} 页'), 'pagination footer must be removed')
  assert.ok(!template.includes('加载更多文档'), 'the list must not use a manual load-more button')
  assert.ok(file.includes('.document-list-scroll {'), 'the list must use the native scroll container')
  assert.ok(file.includes('overflow-y:auto;'), 'the list must scroll vertically inside the page')
  assert.ok(file.includes('grid-template-columns:minmax(0,1fr) 100px minmax(180px,24%) 110px;'), 'document rows must use the native grid columns')
  assert.ok(file.includes('@container (max-width:720px)'), 'the list must collapse secondary columns like the native list')
})

test('document and cleanup lists use compact row density', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  assert.match(file, /\.document-table tbody tr\.document-row \{[\s\S]*?min-height:44px;[\s\S]*?padding:6px 8px;/, 'document rows must use the compact native density')
  assert.match(file, /\.attachment-child \{[\s\S]*?padding:6px 8px 6px 44px;/, 'attachment rows must use the compact density')
  assert.match(file, /\.cleanup-table tbody tr\.cleanup-row \{[\s\S]*?min-height:44px;[\s\S]*?padding:6px 8px;/, 'cleanup rows must use the compact native density')
  assert.match(file, /\.cleanup-table \.cleanup-actions \{[\s\S]*?flex-wrap:nowrap;/, 'cleanup actions must stay on one line')
})

test('document list keeps attachment labels inline and fills the remaining viewport', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="document-title-row"'), 'title and attachment label must share one flex row')
  assert.ok(template.includes("$t('workbench.lifecycle.attachmentTag')"), 'the list must show the localized attachment label')
  assert.ok(template.includes('mappingTagHint(group.parent.mapping_status)'), 'the attachment label must carry its full explanation')
  assert.ok(!template.includes('class="annex-original"'), 'the parent row must not repeat the original file name')
  assert.ok(template.includes("$t('workbench.list.originalFile'"), 'expanded attachment rows show the original file name')
  assert.ok(file.includes('.lifecycle-page--documents { display:flex; flex-direction:column; }'), 'the document tab must use a vertical flex layout')
  assert.ok(file.includes('.lifecycle-page--documents > #panel-documents .document-list-scroll { flex:1; min-height:0; }'), 'the document list must fill the remaining height')
})

test('documents group attachments under their parent and expand them on demand', async () => {
  const view = mountSetup({
    overview: async () => ({
      counts: {}, total: 2, page: 1, page_size: 50,
      documents: [
        { document_id: 'parent', title: '主件', state: 'active', annotations: { effective_from: '', effective_until: '2027-01-01', review_due: '', revision: 1 }, business_family: null, mapping_status: '' },
        { document_id: 'annex', title: '附件中文名', native_title: 'annex-01.docx', annex_original: 'annex-01.docx', parent_document_id: 'parent', attachment_sequence: 1, state: 'active', annotations: { effective_from: '', effective_until: '2027-01-01', review_due: '', revision: 1 }, business_family: null, mapping_status: 'candidate' },
      ],
      rules: { rules: { effective_delay_days: 0, valid_days: 0, review_days: 0 }, source: { tenant_id: 1, knowledge_base_id: 'kb' }, revision: 0 },
      rule_revision: 0, can_write: true,
    }),
  })
  try {
    view.state.kb.value = 'kb'
    await view.state.loadOverview()
    await vue.nextTick()
    assert.equal(view.state.documentGroups.value.length, 1)
    assert.equal(view.state.documentGroups.value[0]?.parent?.document_id, 'parent')
    assert.equal(view.state.documentGroups.value[0]?.children[0]?.document_id, 'annex')
    assert.equal(view.state.mainDocumentCount.value, 1)
    assert.equal(view.state.attachmentCount.value, 1)
    assert.equal(view.state.isDocumentExpanded('parent'), false)
    view.state.toggleDocument('parent')
    assert.equal(view.state.isDocumentExpanded('parent'), true)
    view.state.toggleDocument('parent')
    assert.equal(view.state.isDocumentExpanded('parent'), false)
    view.state.titleQuery.value = '附件中文名'
    await vue.nextTick()
    await vue.nextTick()
    assert.equal(view.state.isDocumentExpanded('parent'), true)
  } finally { view.stop() }
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="tree-toggle"'), 'the parent row needs a chevron toggle')
  assert.ok(template.includes('class="attachment-expand-body"'), 'the attachment list must be an animated expand body')
  assert.ok(file.includes('.attachment-expand-body.is-collapsed { grid-template-rows:0fr; opacity:0; }'), 'collapsed attachments animate closed')
})

test('the feedback sub-tabs write the writable ref, not the readonly kind computed', () => {
  const file = readFileSync(new URL('../business/BusinessView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes("@click=\"feedbackTab='guidance'\""), 'the guidance tab must switch feedbackTab')
  assert.ok(template.includes("@click=\"feedbackTab='feedback'\""), 'the feedback tab must switch feedbackTab')
  assert.ok(!template.includes("@click=\"kind="), 'kind is a readonly computed and must not be assigned')
})

test('the workbench shows feedback review within its workspace', () => {
  const template = parse(readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')).descriptor.template!.content
  assert.doesNotMatch(template, />未归属反馈<\/t-button>/)
  assert.match(template, /id="tab-feedback-review"/)
})

test('workspace pages fill the viewport through the native list height chain', () => {
  const file = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  const shared = readFileSync(new URL('../business/business.css', import.meta.url), 'utf8')
  assert.ok(template.includes("'lifecycle-page--workspace':!!workspaceView"), 'workspace tabs must opt into the vertical shell')
  assert.ok(file.includes('.lifecycle-page--workspace {'), 'the workspace shell must be a flex column')
  assert.ok(file.includes('.lifecycle-page--workspace > #panel-workspace { flex:1; min-height:0; }'), 'the workspace panel must fill remaining height')
  assert.ok(shared.includes('#panel-workspace > .business-page'), 'embedded business pages must share the height chain')
  assert.ok(shared.includes('.workspace-list-scroll {'), 'the shared skeleton must expose a dedicated scroll region')
  assert.ok(shared.includes('position:sticky;'), 'the shared list header must be sticky')
})

test('workspace lists use one compact toolbar and no duplicated page furniture', () => {
  const read = (path: string) => parse(readFileSync(new URL(path, import.meta.url), 'utf8'))
  const business = read('../business/BusinessView.vue')
  const businessTpl = business.descriptor.template?.content ?? ''
  assert.ok(businessTpl.includes('v-if="!scope && !inboxOnly" class="page-heading"'), 'embedded business views must hide the duplicate page heading')
  assert.ok(businessTpl.includes('class="workspace-list-header"'), 'business records need one compact toolbar')
  assert.ok(businessTpl.includes('class="relation-query-dialog"'), 'relation query must open from the toolbar, not take a permanent row')
  assert.ok(businessTpl.includes('class="workspace-pager"'), 'business paging must be compact and inline')
  assert.ok(!businessTpl.includes('<footer class="pagination">'), 'business list must not keep a fixed footer')
  assert.ok(!businessTpl.includes('class="panel relation-query"'), 'relation query must not render as a panel card')

  const family = read('../families/FamilyWorkspaceView.vue')
  const familyTpl = family.descriptor.template?.content ?? ''
  assert.ok(familyTpl.includes('v-if="!scope" class="page-heading"'), 'embedded family view must hide the duplicate page heading')
  assert.ok(familyTpl.includes('class="family-summary"'), 'the family header must collapse into a compact summary strip')
  assert.ok(familyTpl.includes('class="workspace-list-header family-list-header"'), 'family lists need one compact toolbar')
  assert.ok(!familyTpl.includes('<footer class="pagination">'), 'family list must not keep a fixed footer')
  assert.ok(family.descriptor.scriptSetup?.content.includes('currentListCount'), 'family toolbar must count the current list')

  const correction = read('../feedback/CorrectionWorkspace.vue')
  const correctionTpl = correction.descriptor.template?.content ?? ''
  assert.ok(correctionTpl.includes('class="workspace-toolbar correction-toolbar"'), 'correction list needs one compact toolbar')
  assert.ok(correctionTpl.includes('class="correction-filters-advanced"'), 'advanced filters must stay collapsible')
  assert.ok(!correctionTpl.includes('class="correction-filters"'), 'the old permanent filter form must be gone')
  assert.ok(!correctionTpl.includes('class="more-corrections"'), 'load more must move into the toolbar')

  const shared = readFileSync(new URL('../business/business.css', import.meta.url), 'utf8')
  assert.ok(shared.includes('.workspace-toolbar {'), 'the shared skeleton must define the compact toolbar')
  assert.ok(shared.includes('.workspace-list-header .relation-filters {'), 'toolbar filters must lay out inline')
  assert.ok(shared.includes('.relation-query-results {'), 'relation results must scroll inside their own region')
})

test('desktop internal links stay inside the router instead of native anchors', () => {
  const files = [
    ['family history', new URL('../families/FamilyWorkspaceView.vue', import.meta.url)],
    ['binary revision', new URL('../business/BinaryRevisionPanel.vue', import.meta.url)],
    ['correction workspace', new URL('../feedback/CorrectionWorkspace.vue', import.meta.url)],
    ['chat references', new URL('../../components/ChatReferencesDrawer.vue', import.meta.url)],
  ] as const
  for (const [label, url] of files) {
    const file = readFileSync(url, 'utf8')
    assert.ok(!file.includes('<RouterLink'), label + ' must navigate through router.push, not RouterLink anchors')
  }
  const attachment = readFileSync(new URL('../lifecycle/AttachmentWorkbench.vue', import.meta.url), 'utf8')
  assert.ok(!/<t-link[^>]*:href=/.test(attachment), 'attachment title must not carry a native href')
  assert.ok(attachment.includes('@click="router.push(knowledgeRoute'), 'attachment title must navigate through the router')
  const chatStream = readFileSync(new URL('../chat/components/AgentStreamDisplay.vue', import.meta.url), 'utf8')
  assert.ok(chatStream.includes('void router.push(path)'), 'KB citations must use the router in desktop')
  assert.ok(chatStream.includes('await router.push(route)'), 'entity citations must use the router in desktop')
})

test('superseded parent keeps attachment content and only corrects the error text', () => {
  const lifecycle = readFileSync(new URL('./LifecycleView.vue', import.meta.url), 'utf8')
  assert.ok(lifecycle.includes(':parent-state="detail.snapshot.effective_state || detail.snapshot.current.state"'), 'the detail drawer must pass the effective parent state')
  const attachment = readFileSync(new URL('./AttachmentWorkbench.vue', import.meta.url), 'utf8')
  assert.ok(attachment.includes('parentState?: string'), 'the workbench must accept the parent state')
  const failBody = attachment.match(/function fail\(err: unknown\) \{([\s\S]*?)\n\}/)?.[1] ?? ''
  assert.ok(failBody.includes("props.parentState === 'superseded'"), 'error text must be corrected in fail(), not by hiding the workbench')
  assert.ok(failBody.includes('workbench.attachmentWorkbench.messages.parentSuperseded'), 'a superseded parent must explain that attachments are unavailable')
  const loadBody = attachment.match(/async function load\(\) \{([\s\S]*?)\n\}/)?.[1] ?? ''
  assert.ok(loadBody.includes('attachmentWorkbench(props.kb, props.documentId)'), 'attachment content must still load for a superseded parent')
  assert.ok(!loadBody.includes("props.parentState === 'superseded'"), 'load must not skip requests or clear content based on parent state')
})

test('drawer-embedded workspaces expand forms inline and teleport remaining dialogs to body', () => {
  const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
  const family = read('../families/FamilyWorkspaceView.vue')
  const familyTpl = parse(family).descriptor.template?.content ?? ''
  assert.ok(familyTpl.includes('class="family-action-panel"'), 'family actions must expand inline inside the workspace')
  assert.ok(familyTpl.includes('familyActionOpen && activeFamilyAction'), 'the inline panel must keep the existing open state')
  assert.ok(familyTpl.includes('workbench.familyUi.collapseAction'), 'the inline panel must be dismissible')
  assert.ok(!/header="(加入成员|登记下一次更替|新增关联)"/.test(familyTpl), 'family actions must not open modal dialogs inside the drawer')
  for (const [label, file] of [
    ['family', family],
    ['attachment', read('./AttachmentWorkbench.vue')],
    ['business', read('../business/BusinessView.vue')],
  ] as const) {
    const dialogs = (file.match(/<t-dialog/g) ?? []).length
    const attached = (file.match(/attach="body"/g) ?? []).length
    assert.ok(dialogs > 0, label + ' must expose dialog markup to verify')
    assert.equal(attached, dialogs, label + ' dialogs must teleport to body so the drawer transform cannot trap them')
  }
})
