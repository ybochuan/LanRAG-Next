import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as businessModel from '../business/model'
import * as lifecycleModel from '../lifecycle/model'
import type { BusinessBody } from '@/api/business'

function setup(workspace: (kb:string,id:string) => Promise<unknown>, loseFirstResponse=false) {
  const script = parse(readFileSync(new URL('./FamilyWorkspaceView.vue',import.meta.url),'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(script,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
  const scope = vue.effectScope()
  const calls: {kb:string;action:string;body:BusinessBody}[] = []
  let approved = false
  const prompts: string[] = []
  const modules: Record<string,unknown> = {
    vue: {...vue,onMounted(){},onBeforeUnmount(){}},
    'vue-router': {useRoute:()=>({query:{}}),onBeforeRouteLeave(){}},
    '@/stores/auth': {useAuthStore:()=>({effectiveTenantId:7})},
    '@/api/families': {workspace,listFamilies:async()=>[]},
    '@/api/knowledge-base': {},
    '@/api/lifecycle': {overview:async()=>({can_write:true})},
    '@/api/business': {documentTitles:async()=>({}),write:async(kb:string,action:string,body:BusinessBody)=>{
      calls.push({kb,action,body})
      if (loseFirstResponse && calls.length===1) throw new Error('fixture response lost')
      return {id:'operation',status:'scheduled'}
    }},
    '../business/model':businessModel,'../lifecycle/model':lifecycleModel,
    '../business/CreateForm.vue':{},'../business/DocumentPicker.vue':{},'./FamilyDocumentPanel.vue':{},'../business/business.css':{},
  }
  const window = {confirm(message:string){prompts.push(message);return approved}}
  const state = scope.run(()=>new Function('require','exports','window','defineProps','defineEmits',`${code}
    return {kb,familyId,family,canWrite,members,loadView,approve,confirmSuggestions,perform,pending};
  `)((id:string)=>modules[id],{},window,()=>({}),()=>()=>{}))
  return {state,calls,prompts,allow:()=>{approved=true},stop:()=>scope.stop()}
}

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
      old_document_id:'old-policy',effective_at:'2026-11-15T00:00:00Z',revision:3}
    view.state.canWrite.value=true
    view.state.approve(row)
    assert.equal(view.calls.length,0)
    assert.match(view.prompts[0]!, /old-policy.*退出普通检索/)
    view.allow()
    view.state.canWrite.value=false
    view.state.approve(row)
    assert.equal(view.calls.length,0)
    view.state.canWrite.value=true
    view.state.approve(row)
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
    view.state.approve(row)
    view.state.approve({...row,family_revision:7,old_document_id:'2020',old_version_node_id:'v2020'})
    assert.equal(view.prompts.length,0)
    assert.equal(view.calls.length,0)
  } finally { view.stop() }
})
