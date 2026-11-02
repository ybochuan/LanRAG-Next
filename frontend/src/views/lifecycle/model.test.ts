import assert from 'node:assert/strict'
import test from 'node:test'
import { annotationsForSave, toLocalInput, requestFence, validRules, ruleSourceLabel, supplementCommand } from './model'

test('date edit preserves invisible labels and sends UTC while cleared dates are null', () => {
 const result=annotationsForSave({revision:3,labels:{classification:'private',unrelated:'keep'}}, {effective_from:'',effective_until:'2026-11-01T12:30',review_due:''})
 assert.deepEqual(result.labels,{classification:'private',unrelated:'keep'})
 assert.equal(result.effective_from,null)
 assert.equal(result.review_due,null)
 assert.equal(result.effective_until,new Date('2026-11-01T12:30').toISOString())
 assert.equal(toLocalInput(result.effective_until!), '2026-11-01T12:30')
})
test('late responses from a previous knowledge base cannot update current view', () => {
 const fence=requestFence();const first=fence.begin();fence.invalidate();const second=fence.begin()
 assert.equal(fence.current(first),false);assert.equal(fence.current(second),true)
})
test('rules reject fractional, missing and negative day counts',()=>{
 assert.equal(validRules({effective_delay_days:0,valid_days:36500,review_days:7}),true)
 for(const days of [-1,1.5,NaN,Infinity,36501])assert.equal(validRules({effective_delay_days:0,valid_days:days,review_days:0}),false)
})
test('untouched dates retain their exact server precision and timezone',()=>{
 const original={revision:4,labels:{},effective_until:'2026-11-02T03:04:59.123456789Z'}
 assert.equal(annotationsForSave(original,{effective_from:'',review_due:'',effective_until:toLocalInput(original.effective_until)}).effective_until,original.effective_until)
})
test('unconfigured rules are distinguished from a KB override',()=>{
 const rules={rules:{effective_delay_days:0,valid_days:0,review_days:0},source:{tenant_id:7,knowledge_base_id:'kb'},revision:0,token:'fixture'}
 assert.equal(ruleSourceLabel(rules),'未配置（永久有效）')
 assert.equal(ruleSourceLabel({...rules,revision:1}),'知识库规则')
 assert.equal(ruleSourceLabel({...rules,revision:1,source:{tenant_id:7,knowledge_base_id:''}}),'租户默认规则')
})

test('supplement save binds displayed revision and operation and rejects unsafe selections',()=>{
 assert.deepEqual(supplementCommand(3,['parent-b','parent-a'],'child','retry-id'),{expected_revision:3,operation_id:'retry-id',parents:['parent-a','parent-b']})
 assert.deepEqual(supplementCommand(3,[],'child','revoke').parents,[])
 for(const parents of [['child'],['parent','parent'],Array.from({length:9},(_,i)=>`parent-${i}`)])assert.throws(()=>supplementCommand(3,parents,'child','op'))
})
