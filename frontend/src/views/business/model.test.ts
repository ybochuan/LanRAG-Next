import assert from 'node:assert/strict'
import test from 'node:test'
import { actionsFor, buildIntent, businessError, canActivate } from './model'

test('actions require write access and use the actual workflow state',()=>{
 const row={id:'r',revision:3,status:'candidate'}
 assert.deepEqual(actionsFor('relation',row,false),[])
 assert.deepEqual(actionsFor('relation',row,true),['relation_confirm','relation_revoke'])
 assert.deepEqual(actionsFor('revision',{...row,status:'published'},true),[])
 assert.deepEqual(actionsFor('revision',{...row,status:'publish_failed'},true),['revision_publish'])
})
test('activation respects the server deadline, including high risk',()=>{
 const row={id:'f',revision:2,status:'approved',risk:'high',ready_at:'2026-11-05T00:00:00Z'}
 assert.equal(canActivate(row,Date.parse('2026-11-04T00:00:00Z')),false)
 assert.equal(canActivate(row,Date.parse('2026-11-05T00:00:00Z')),true)
 assert.equal(canActivate({...row,ready_at:null},Date.now()),false)
})
test('retry retains the displayed revision, operation and immutable payload',()=>{
 const payload={document_id:'doc',guidance_ids:['g'],text:'new Markdown'}
 const intent=buildIntent('revision_create',undefined,payload,'original-operation')
 payload.guidance_ids.push('later');payload.text='edited after request'
 assert.deepEqual(intent.body.business.payload.guidance_ids,['g'])
 assert.equal(intent.body.business.payload.text,'new Markdown')
 assert.equal(intent.body.operation_id,'original-operation')
 const update=buildIntent('revision_approve',{id:'p',revision:8,status:'pending_review'},{},'approve')
 assert.equal(update.body.expected_revision,8);assert.equal(update.body.business.id,'p')
 assert.throws(()=>buildIntent('revision_create',undefined,{...payload,text:'你'.repeat(16001)},'too-large'))
})
test('errors describe conflict or unknown outcome without inventing rollback',()=>{
 assert.match(businessError({$httpStatus:409}),/重新载入/)
 assert.match(businessError({response:{status:403}}),/权限/)
 assert.match(businessError({$httpStatus:503}),/原请求/)
})
