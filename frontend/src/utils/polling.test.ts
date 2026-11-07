import assert from 'node:assert/strict'
import test from 'node:test'
import {
  addPageVisibilityListener,
  DOCUMENT_STATUS_POLL_BACKOFF_MS,
  isPageHidden,
  MOVE_PROGRESS_POLL_BACKOFF_MS,
  pollingBackoffDelay,
  WIKI_STATUS_POLL_BACKOFF_MS,
} from './polling.ts'

test('polling backoff follows each fixed schedule and clamps at the last value', () => {
  assert.deepEqual([...DOCUMENT_STATUS_POLL_BACKOFF_MS], [1500, 3000, 6000, 12000, 30000, 60000])
  assert.deepEqual([...WIKI_STATUS_POLL_BACKOFF_MS], [5000, 10000, 20000, 60000])
  assert.deepEqual([...MOVE_PROGRESS_POLL_BACKOFF_MS], [2000, 5000, 10000, 30000])

  assert.equal(pollingBackoffDelay(0, DOCUMENT_STATUS_POLL_BACKOFF_MS), 1500)
  assert.equal(pollingBackoffDelay(1, DOCUMENT_STATUS_POLL_BACKOFF_MS), 3000)
  assert.equal(pollingBackoffDelay(5, DOCUMENT_STATUS_POLL_BACKOFF_MS), 60000)
  assert.equal(pollingBackoffDelay(99, DOCUMENT_STATUS_POLL_BACKOFF_MS), 60000)
  assert.equal(pollingBackoffDelay(-1, DOCUMENT_STATUS_POLL_BACKOFF_MS), 1500)
})

test('page visibility helpers read visibilityState and remove their listener', () => {
  const originalDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const fakeDocument = Object.assign(new EventTarget(), { visibilityState: 'visible' })
  Object.defineProperty(globalThis, 'document', { value: fakeDocument, configurable: true })
  try {
    let calls = 0
    const dispose = addPageVisibilityListener(() => { calls++ })

    assert.equal(isPageHidden(), false)
    fakeDocument.visibilityState = 'hidden'
    assert.equal(isPageHidden(), true)
    fakeDocument.dispatchEvent(new Event('visibilitychange'))
    assert.equal(calls, 1)

    dispose()
    fakeDocument.dispatchEvent(new Event('visibilitychange'))
    assert.equal(calls, 1)
  } finally {
    if (originalDocument) Object.defineProperty(globalThis, 'document', originalDocument)
    else delete (globalThis as { document?: unknown }).document
  }
})
