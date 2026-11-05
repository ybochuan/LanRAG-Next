import assert from 'node:assert/strict'
import test from 'node:test'

import { SupervisorError, isAuthFailure } from './modelSupervisor.ts'

test('isAuthFailure recognises a rejected supervisor token', () => {
  assert.equal(
    isAuthFailure(new SupervisorError('missing or invalid credential', 401, 'unauthorized')),
    true
  )
  // Some proxies answer with their own 403 but keep the credential meaning.
  assert.equal(isAuthFailure(new SupervisorError('unauthorized', 403, 'unauthorized')), true)
})

test('isAuthFailure leaves other failures to the normal error slot', () => {
  assert.equal(isAuthFailure(new SupervisorError('insufficient vram', 428, 'insufficient_vram')), false)
  // status 0 is the supervisor-down / CORS case; it is not a credential problem.
  assert.equal(isAuthFailure(new SupervisorError('无法连接模型管理器', 0, 'unreachable')), false)
  assert.equal(isAuthFailure(new Error('boom')), false)
  assert.equal(isAuthFailure(undefined), false)
})
