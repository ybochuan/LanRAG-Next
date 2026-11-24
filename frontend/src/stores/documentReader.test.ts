import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import * as pinia from 'pinia'
import ts from 'typescript'
import * as vue from 'vue'
import * as model from '../views/document-reader/model'
import type * as readerStore from './documentReader'

const source = readFileSync(new URL('./documentReader.ts', import.meta.url), 'utf8')
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
function setup() {
  const auth = vue.reactive({ currentUserId: 'reviewer', effectiveTenantId: 1 })
  const modules: Record<string, unknown> = { pinia, vue, '@/views/document-reader/model': model, './auth': { useAuthStore: () => auth } }
  const exports = {} as typeof readerStore
  new Function('require', 'exports', code)((id: string) => modules[id], exports)
  const scope = pinia.createPinia()
  return { auth, session: exports.useDocumentReaderStore(scope), workbenchTarget: exports.workbenchTarget, stop: () => pinia.disposePinia(scope) }
}

test('return only accepts the matching token and exact path, then consumes it', () => {
  const { session, stop, workbenchTarget } = setup()
  try {
    const target = workbenchTarget({ kb: 'kb', view: 'families', unrelated: 'https://example.test', family: ['a','b'] }, '#panel')
    assert.deepEqual(target, { name: 'documents', query: { kb: 'kb', view: 'families' }, hash: '#panel' })
    const snapshot: readerStore.WorkbenchReturn = {
      path: '/platform/documents?kb=kb&view=families&unrelated=extra#panel', targetPath: '/platform/documents?kb=kb&view=families#panel', target,
      left: { kb: 'kb', id: 'old', version: 'v1' }, right: { kb: 'kb', id: 'new' }, source: { kind: 'family', family: 'family', record: 'replacement' },
      kb: 'kb', area: 'main', scroll: 120, documentScroll: 0, pages: 1, sort: { key: 'title', direction: -1 }, expanded: [], reviewSort: 'asc', collapsedTaskSections: [],
    }
    const token = session.captureWorkbench(snapshot)
    assert.equal(session.takeWorkbench(snapshot.path, undefined), undefined)
    assert.equal(session.takeWorkbench(snapshot.path, 'other'), undefined)
    assert.equal(session.takeWorkbench('/platform/documents?kb=other', token), undefined)
    assert.deepEqual(session.takeWorkbench(snapshot.targetPath, token), { ...snapshot, id: token })
    assert.equal(session.takeWorkbench(snapshot.path, token), undefined)
    const backToken = session.captureWorkbench(snapshot)
    assert.deepEqual(session.takeWorkbench(snapshot.path, backToken), { ...snapshot, id: backToken })
  } finally { stop() }
})

test('scope changes clear return snapshots before a Back event can consume them', () => {
  const { auth, session, stop, workbenchTarget } = setup()
  try {
    const path = '/platform/documents?kb=kb'
    const snapshot: readerStore.WorkbenchReturn = {
      path, targetPath: path, target: workbenchTarget({ kb: 'kb' }, ''), kb: 'kb', area: 'drawer',
      left: { kb: 'kb', id: 'doc' }, source: { kind: 'document' },
      scroll: 0, documentScroll: 0, pages: 1, sort: { key: 'title', direction: 1 }, expanded: [], reviewSort: 'asc', collapsedTaskSections: [],
    }
    const token = session.captureWorkbench(snapshot)
    auth.effectiveTenantId++
    assert.equal(session.takeWorkbench(path, token), undefined)
    const userToken = session.captureWorkbench(snapshot)
    auth.currentUserId = 'other'
    assert.equal(session.takeWorkbench(path, userToken), undefined)
  } finally { stop() }
})
