import assert from 'node:assert/strict'
import test from 'node:test'

import { SupervisorError, isAuthFailure, list } from './modelSupervisor.ts'

test('packaged Desktop uses the WebView path without a browser credential', async (t) => {
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: { __RUNTIME_CONFIG__: { LANRAG_LITE_PRODUCT: true } },
  })
  t.after(() => { Reflect.deleteProperty(globalThis, 'window') })
  t.mock.method(globalThis, 'fetch', async (path: string, init: RequestInit) => {
    assert.equal(path, '/extensions/models/list')
    assert.deepEqual(init.headers, { 'X-AgentRAG-Model-Supervisor-Version': '1' })
    return Response.json({ instances: [] })
  })
  assert.deepEqual(await list(), { instances: [] })
})

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
