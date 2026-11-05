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

const answer = {
  id: 'client-segment', assistant_message_id: 'real-answer', is_completed: true, content: 'Answer',
  isRagMode: true, isAgentMode: true,
  knowledge_references: [{ knowledge_base_id: 'kb-one' }, { knowledge_base_id: 'kb-one' }],
}

function setup(message: Record<string, unknown>, write: (...args: unknown[]) => Promise<unknown>) {
  const exports: { default?: { setup: (props: object, context: object) => any } } = {}
  let counter = 0
  runInNewContext(script, {
    exports,
    TextEncoder,
    crypto: { getRandomValues: (value: Uint32Array) => value.fill(++counter) },
    require: (name: string) => ({
      vue,
      'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
      '@/api/business': { captureAnswerFeedback: write },
      '@/utils/steerStreamFork': messageIdentity,
    } as Record<string, unknown>)[name],
  })
  return exports.default!.setup({ message, sessionId: 'session-one' }, { expose() {} })
}

test('only a completed, non-fallback quick answer with a single referenced KB offers feedback', async () => {
  let submissions = 0
  const write = async () => { submissions++ }
  assert.equal(setup(answer, write).target.value.messageId, 'real-answer')
  for (const message of [
    { ...answer, isRagMode: false },
    { ...answer, is_completed: false },
    { ...answer, is_fallback: true },
    { ...answer, content: '' },
    { ...answer, knowledge_references: [{ knowledge_base_id: 'kb-one' }, { knowledge_base_id: 'kb-two' }] },
  ]) {
    const state = setup(message, write)
    assert.equal(state.target.value, undefined)
    await state.submit()
  }
  assert.equal(submissions, 0)
})

test('an uncertain submission retries the original operation, answer, and feedback', async () => {
  const calls: string[] = []
  const state = setup(answer, async (...args) => {
    calls.push(JSON.stringify(args))
    if (calls.length === 1) throw { $httpStatus: 503 }
    return { id: 'feedback', status: 'pending_review' }
  })
  state.topic.value = ' Check the effective date '
  state.details.value = 'The answer used the older revision.'
  state.incorrect.value = true
  await state.submit()
  assert.equal(state.error.value, 'chat.answerFeedback.retry')
  assert.equal(state.submitted.value, false)
  assert.ok(state.intent.value)
  const [kb, operation, payload] = JSON.parse(calls[0]!)
  assert.equal(kb, 'kb-one')
  assert.match(operation, /^qa-feedback-/)
  assert.deepEqual(payload, {
    session_id: 'session-one', message_id: 'real-answer', topic: 'Check the effective date',
    text: 'The answer used the older revision.', negative: true,
  })
  state.details.value = 'An unsent edit must not change the retried payload.'
  await state.submit()
  await state.submit()
  assert.deepEqual(calls, [calls[0], calls[0]])
  assert.equal(state.submitted.value, true)
})
