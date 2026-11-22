import assert from 'node:assert/strict'
import test from 'node:test'
import { sampleReviewPending, actionsFor, buildIntent, businessError, canActivate, statusLabels } from './model'

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
test('repeal schedules require two documents, an explicit time, and a separate approval', () => {
 const row = { id: 'r', revision: 2, status: 'pending_review', edge_type: 'repeals' }
 assert.deepEqual(actionsFor('relation', row, true), ['repeal_approve', 'repeal_cancel'])
 assert.deepEqual(actionsFor('relation', row, false), [])
 assert.deepEqual(actionsFor('relation', { ...row, status: 'scheduled' }, true), ['repeal_cancel'])
 assert.deepEqual(actionsFor('relation', { ...row, status: 'applied' }, true), [])
 const payload = { document_id: 'notice', target_document_id: 'old', effective_at: '2026-11-08T09:00:00.000Z' }
 assert.throws(() => buildIntent('repeal_create', undefined, { ...payload, effective_at: '' }, 'missing-time'))
 assert.equal(buildIntent('repeal_create', undefined, payload, 'schedule').body.business.payload.effective_at, payload.effective_at)
})
test('clause adjudication requires a reviewed, still-current conflict and a reason', () => {
 const row = { id: 'c', revision: 2, status: 'awaiting_resolution', edge_type: 'conflicts_with', pins_current: true }
 assert.deepEqual(actionsFor('relation', row, true), ['clause_resolve', 'clause_revoke'])
 assert.deepEqual(actionsFor('relation', { ...row, pins_current: false }, true), ['clause_revoke'])
 assert.deepEqual(actionsFor('relation', row, false), [])
 assert.throws(() => buildIntent('clause_resolve', row, { resolution: 'prefer_source', text: '' }, 'missing-note'))
 assert.equal(buildIntent('clause_resolve', row, { resolution: 'contextual', text: 'Context B applies.' }, 'decision').body.expected_revision, 2)
})
test('split review applies to the batch and each approved part can resume publishing', () => {
 const batch = { id: 'split', revision: 1, status: 'pending_review', record_kind: 'split_batch' as const }
 const part = { id: 'part', revision: 2, status: 'pending_review', record_kind: 'split_part' as const }
 assert.deepEqual(actionsFor('revision', batch, true), ['split_approve', 'split_reject'])
 assert.deepEqual(actionsFor('revision', batch, false), [])
 assert.deepEqual(actionsFor('revision', { ...batch, status: 'approved' }, true), [])
 assert.deepEqual(actionsFor('revision', part, true), [])
 assert.deepEqual(actionsFor('revision', { ...part, status: 'publish_failed' }, true), ['revision_publish'])
})
test('split submission requires complete parts and preserves the approved contents on retry', () => {
 const first = { title: '制度甲', text: '完整正文甲' }
 const second = { title: '制度乙', text: '完整正文乙' }
 const payload = { document_id: 'doc', guidance_ids: ['g'], split_parts: [first, second] }
 assert.throws(() => buildIntent('split_create', undefined, { ...payload, split_parts: [first] }, 'one'))
 assert.throws(() => buildIntent('split_create', undefined, { ...payload, split_parts: [first, { ...second, text: ' ' }] }, 'blank'))
 const intent = buildIntent('split_create', undefined, payload, 'submit-split')
 second.text = '修改后的正文'
 assert.deepEqual(intent.body.business.payload.split_parts, [first, { title: '制度乙', text: '完整正文乙' }])
 assert.equal(Object.isFrozen(intent.body.business.payload.split_parts?.[1]), true)
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
test('historical and removed statuses use the workbench vocabulary',()=>{
 assert.equal(statusLabels.historical,'历史记录')
 assert.equal(statusLabels.removed,'已移出')
})

test('errors describe conflict or unknown outcome without inventing rollback',()=>{
 assert.match(businessError({$httpStatus:409}),/重新载入/)
 assert.doesNotMatch(businessError({$httpStatus:409}),/冷静期|主题复核/)
 assert.match(businessError({response:{status:403}}),/权限/)
 assert.match(businessError({$httpStatus:503}),/原请求/)
})

test('sample review is pending only for an active feedback that was sampled', () => {
  const pending = { id: 'f1', revision: 1, status: 'active', sample_decision: 'pending' } as any
  assert.equal(sampleReviewPending(pending), true)
  assert.equal(sampleReviewPending({ ...pending, status: 'approved' }), false)
  assert.equal(sampleReviewPending({ ...pending, sample_decision: 'passed' }), false)
  assert.equal(sampleReviewPending({ ...pending, sample_decision: null }), false)
})
