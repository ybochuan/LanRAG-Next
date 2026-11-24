import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import { translate } from '../../test-utils/i18n'
import * as businessModel from '../business/model'
import * as lifecycleModel from '../lifecycle/model'
import type { BusinessBody } from '@/api/business'

function setup(workspace: (kb:string,id:string) => Promise<unknown>, loseFirstResponse=false, props: Record<string, unknown> = {}) {
  const script = parse(readFileSync(new URL('./FamilyWorkspaceView.vue',import.meta.url),'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(script,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
  const scope = vue.effectScope()
  const calls: {kb:string;action:string;body:BusinessBody}[] = []
  let approved = false
  let overviewCalls = 0
  const prompts: string[] = []
  const events: unknown[][] = []
  const modules: Record<string,unknown> = {
    vue: {...vue,onMounted(){},onBeforeUnmount(){}},
    'vue-router': {useRoute:()=>({query:{}}),useRouter:()=>({push(){}}),onBeforeRouteLeave(){}},
    '@/stores/auth': {useAuthStore:()=>({effectiveTenantId:7})},
    'vue-i18n': {useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => translate(key, params), locale: { value: 'zh-CN' } })},
    '@/api/families': {workspace,listFamilies:async()=>[]},
    '@/api/knowledge-base': {},
    '@/api/lifecycle': {overview:async()=>{overviewCalls++;return {can_write:true}}},
    '@/api/business': {documentTitles:async()=>({}),write:async(kb:string,action:string,body:BusinessBody)=>{
      calls.push({kb,action,body})
      if (loseFirstResponse && calls.length===1) throw new Error('fixture response lost')
      return {id:'operation',status:'scheduled'}
    }},
    '../business/model':businessModel,'../lifecycle/model':lifecycleModel,
    '@/utils/confirm': {confirmAction: async(options:{body:string})=>{prompts.push(options.body);return approved}},
    '../business/CreateForm.vue':{},'../business/DocumentPicker.vue':{},'./FamilyDocumentPanel.vue':{},'../business/business.css':{},
  }
  const window = {confirm(message:string){prompts.push(message);return approved}}
  const state = scope.run(()=>new Function('require','exports','window','defineProps','defineEmits','defineExpose',`${code}
    return {kb,familyId,family,canWrite,members,recoverDocument,recoverReason,loadFamilies,loadView,recoverHead,approve,confirmSuggestions,perform,pending,compareReplacement,readerState};
  `)((id:string)=>modules[id],{},window,()=>props,()=>(...args:unknown[])=>events.push(args),()=>{}))
  return {state,calls,prompts,events,allow:()=>{approved=true},overviewCalls:()=>overviewCalls,stop:()=>scope.stop()}
}

test('a replacement compares the actual old and new documents with the known old pin', async () => {
  const view = setup(async (_, id) => snapshot(id))
  try {
    view.state.kb.value = 'kb-family'; await vue.nextTick()
    view.state.compareReplacement({ id: 'replacement', business_family_id: 'family', old_document_id: 'old', old_version_node_id: 'version-old', new_document_id: 'new' })
    assert.deepEqual(view.events.at(-1), ['read', {
      left: { kb: 'kb-family', id: 'old', version: 'version-old' }, right: { kb: 'kb-family', id: 'new' },
      source: { kind: 'family', family: 'family', record: 'replacement', historyPreview: false },
    }])
  } finally { view.stop() }
})

const snapshot = (id: string) => ({family:{business_family_id:id,revision:2},can_write:true,members:[{document_id:id}],has_next:false})

test('an older workbench response cannot reveal previous-knowledge-base members',async()=>{
  const delayed: Array<(value: unknown)=>void> = []
  const view=setup((kb,id)=>kb==='old' ? new Promise(resolve=>delayed.push(resolve)) : Promise.resolve(snapshot(id)))
  try {
    view.state.kb.value='old'
    await vue.nextTick()
    view.state.familyId.value='previous-family'
    const older=view.state.loadView()
    view.state.kb.value='new'
    await vue.nextTick()
    view.state.familyId.value='current-family'
    await vue.nextTick()
    await view.state.loadView()
    delayed.forEach(release=>release(snapshot('previous-family')))
    await older
    assert.equal(view.state.family.value.business_family_id,'current-family')
    assert.deepEqual(view.state.members.value,[{document_id:'current-family'}])
  } finally { view.stop() }
})

test('batch retries only its unresolved operation and keeps the original receipt key',async()=>{
  const view=setup(async(_,id)=>snapshot(id),true)
  try {
    view.state.kb.value='kb'
    await vue.nextTick()
    view.state.canWrite.value=true
    view.allow()
    await view.state.confirmSuggestions([
      {id:'first',kind:'membership',revision:3},
      {id:'second',kind:'relationship',edge_type:'related_to',revision:4},
    ])
    assert.equal(view.calls.length,1)
    assert.equal(view.state.pending.value.length,2)
    await view.state.perform()
    assert.deepEqual(view.calls.map(call=>call.body.business.id),['first','first','second'])
    assert.equal(view.calls[0]?.body.operation_id,view.calls[1]?.body.operation_id)
    assert.notEqual(view.calls[1]?.body.operation_id,view.calls[2]?.body.operation_id)
    assert.equal(view.state.pending.value,undefined)
  } finally { view.stop() }
})

test('approving replacement requires confirmation and a write grant',async()=>{
  const view=setup(async(_,id)=>snapshot(id))
  try {
    view.state.kb.value='kb'
    await vue.nextTick()
    view.state.family.value={business_family_id:'family',revision:2}
    const row={id:'proposal',business_family_id:'family',family_revision:2,status:'pending_review',
      old_document_id:'old-policy',effective_at:'2099-11-15T00:00:00Z',revision:3}
    view.state.canWrite.value=true
    await view.state.approve(row)
    assert.equal(view.calls.length,0)
    assert.match(view.prompts[0]!, /old-policy.*退出普通检索/)
    view.allow()
    view.state.canWrite.value=false
    await view.state.approve(row)
    assert.equal(view.calls.length,0)
    view.state.canWrite.value=true
    await view.state.approve(row)
    assert.equal(view.calls.length,1)
    assert.equal(view.calls[0]?.action,'family_replace_approve')
    assert.equal(view.calls[0]?.body.expected_revision,3)
    assert.equal(view.calls[0]?.body.business.payload.second_confirmation,true)
  } finally { view.stop() }
})

test('a stale replacement or a replacement ahead of its predecessor cannot prompt for approval', async () => {
  const view=setup(async(_,id)=>snapshot(id))
  try {
    view.state.kb.value='kb'
    await vue.nextTick()
    view.state.canWrite.value=true
    view.state.family.value={business_family_id:'family',revision:7,published_document_id:'2018',published_version_node_id:'v2018'}
    view.allow()
    const row={id:'proposal',business_family_id:'family',status:'pending_review',family_revision:6,
      old_document_id:'2018',old_version_node_id:'v2018',new_document_id:'2020',effective_at:'2020-01-01T00:00:00Z',revision:1}
    await view.state.approve(row)
    await view.state.approve({...row,family_revision:7,old_document_id:'2020',old_version_node_id:'v2020'})
    assert.equal(view.prompts.length,0)
    assert.equal(view.calls.length,0)
  } finally { view.stop() }
})

test('missing published head can be recovered through an explicit member selection', async () => {
  const view=setup(async()=>[])
  try {
    view.state.kb.value='kb'
    await vue.nextTick()
    view.state.canWrite.value=true
    view.state.family.value={business_family_id:'family',revision:4,published_document_id:'old',published_head_availability:'missing'}
    view.state.members.value=[{document_id:'new',availability:'active'}]
    view.state.recoverDocument.value='new'
    view.state.recoverReason.value='old source deleted'
    view.allow()
    await view.state.recoverHead()
    assert.equal(view.calls.length,1)
    assert.equal(view.calls[0]?.action,'family_head_recover')
    assert.equal(view.calls[0]?.body.expected_revision,4)
    assert.deepEqual(view.calls[0]?.body.business.payload,{document_id:'new',reason:'old source deleted',second_confirmation:true})
  } finally { view.stop() }
})

test('embedded workspace reuses the authority supplied by LifecycleView without loading overview', async () => {
  const view=setup(async()=>[], false, {scope:'kb', canWrite:true})
  try {
    view.state.kb.value='kb'
    await vue.nextTick()
    await view.state.loadFamilies()
    assert.equal(view.state.canWrite.value,true)
    assert.equal(view.overviewCalls(),0)
  } finally { view.stop() }
})

test('family tabs render through the native workbench list skeleton', () => {
  const file = readFileSync(new URL('./FamilyWorkspaceView.vue', import.meta.url), 'utf8')
  const template = parse(file).descriptor.template?.content ?? ''
  assert.ok(template.includes('class="workspace-list-panel family-list-panel"'), 'the family tab body must be a transparent native list panel')
  assert.ok(template.includes('class="workspace-list-head"'), 'family lists need a sticky column header')
  assert.ok(template.includes('class="workspace-list-row family-row"'), 'family members, relations, plans and activity use grid rows')
  assert.ok(template.includes('class="workspace-list-scroll family-list-scroll"'), 'only the family list should scroll')
  assert.ok(!template.includes('<ul v-if="members.length" class="family-items">'), 'members must not use the old plain list')
})
