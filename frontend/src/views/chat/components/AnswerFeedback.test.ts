import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as messageIdentity from '../../../utils/steerStreamFork'

const { descriptor } = parse(readFileSync(new URL('./AnswerFeedback.vue', import.meta.url), 'utf8'))
const script = ts.transpileModule(compileScript(descriptor, { id: 'answer-feedback-test' }).content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS },
}).outputText

const answer = { id: 'client-segment', assistant_message_id: 'real-answer', is_completed: true, content: 'Visible answer', role: 'assistant' }
const snapshot = { question: 'Persisted question', answer_excerpt: 'Persisted answer', answer_truncated: true, snapshot_digest: 'a'.repeat(64) }

function setup(
  message: Record<string, unknown>,
  write: (...args: unknown[]) => Promise<unknown>,
  preview: (...args: unknown[]) => Promise<unknown> = async () => snapshot,
) {
  const exports: { default?: { setup: (props: object, context: object) => any } } = {}
  let counter = 0
  runInNewContext(script, {
    exports,
    TextEncoder,
    crypto: { getRandomValues: (value: Uint32Array) => value.fill(++counter) },
    require: (name: string) => ({
      vue,
      'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
      '@/api/business': { captureAnswerFeedback: write, previewAnswerFeedback: preview },
      '@/utils/steerStreamFork': messageIdentity,
    } as Record<string, unknown>)[name],
  })
  const props = vue.reactive({ message, sessionId: 'session-one' })
  return { props, state: exports.default!.setup(props, { expose() {} }) }
}

test('quick, agent, fallback and multiple-source answers can all offer feedback', async () => {
  const write = async () => assert.fail('should not submit an ineligible answer')
  for (const message of [
    { ...answer, isRagMode: true },
    { ...answer, isAgentMode: true },
    { ...answer, is_fallback: true },
    { ...answer, knowledge_references: [{ knowledge_base_id: 'one' }, { knowledge_base_id: 'two' }] },
  ]) {
    assert.equal(setup(message, write).state.target.value.messageId, 'real-answer')
  }
  for (const message of [
    { ...answer, is_completed: false },
    { ...answer, steerForked: true },
    { ...answer, role: 'user' },
    { ...answer, assistant_message_id: '', id: '' },
    { ...answer, content: '' },
  ]) {
    const { state } = setup(message, write)
    assert.equal(state.target.value, undefined)
    await state.submit()
  }
})

test('an uncertain submission retries the same operation and only sends user feedback', async () => {
  const calls: string[] = []
  const previews: unknown[][] = []
  const { state } = setup(answer, async (...args) => {
    calls.push(JSON.stringify(args))
    if (calls.length === 1) throw { $httpStatus: 503 }
  }, async (...args) => { previews.push(args); return snapshot })
  await state.submit()
  assert.equal(calls.length, 0)
  await state.loadPreview()
  assert.deepEqual(previews, [['session-one', 'real-answer']])
  assert.deepEqual(state.snapshot.value, snapshot)
  await state.submit()
  assert.equal(state.error.value, 'chat.answerFeedback.invalid')
  state.details.value = ' The answer used the older revision. '
  await state.submit()
  assert.equal(state.error.value, 'chat.answerFeedback.retry')
  assert.equal(state.submitted.value, false)
  const [session, message, payload] = JSON.parse(calls[0]!)
  assert.equal(session, 'session-one')
  assert.equal(message, 'real-answer')
  assert.match(payload.operation_id, /^qa-feedback-/)
  assert.deepEqual(payload, { operation_id: payload.operation_id, rating: 'correction', text: 'The answer used the older revision.', snapshot_digest: snapshot.snapshot_digest })
  state.details.value = 'An unsent edit must not change the retried payload.'
  await state.submit()
  await state.submit()
  assert.deepEqual(calls, [calls[0], calls[0]])
  assert.equal(state.submitted.value, true)
})

test('restricted previews accept a rating and reason without sharing draft text', async () => {
  const calls: unknown[][] = []
  const { state } = setup(answer, async (...args) => { calls.push(args) }, async () => ({ snapshot_restricted: true, snapshot_digest: snapshot.snapshot_digest }))
  state.details.value = 'Unshared details'
  await state.loadPreview()
  assert.equal(state.rating.value, 'dislike')
  state.reason.value = 'missing_sources'
  await state.submit()
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0]?.[2])), {
    operation_id: 'qa-feedback-1-1-1-1', rating: 'dislike', reason: 'missing_sources', snapshot_digest: snapshot.snapshot_digest,
  })
})

test('an old preview arriving after a message change cannot be submitted', async () => {
  let finish: (value: typeof snapshot) => void = () => {}
  const pending = new Promise<typeof snapshot>(resolve => { finish = resolve })
  const { props, state } = setup(answer, async () => assert.fail('should not submit old snapshot'), async () => pending)
  const loading = state.loadPreview()
  props.message = { ...answer, assistant_message_id: 'new-answer' }
  finish(snapshot)
  await loading
  assert.equal(state.snapshot.value, undefined)
  assert.equal(state.previewLoading.value, false)
  await state.submit()
})
