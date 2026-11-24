import assert from 'node:assert/strict'
import test from 'node:test'
import { readerIntent, readerKey, readerQuery, relationGroup } from './model'

test('keeps both comparison scopes and pinned version identities', () => {
  const left = { kb: 'kb-a', id: 'doc-a', version: 'version:one' }
  const right = { kb: 'kb-b', id: 'doc-b', version: 'version:two' }
  const query = { kb: left.kb, doc: left.id, version: left.version, compare: right.id, compareKB: right.kb, compareVersion: right.version }
  assert.deepEqual(readerQuery({ left, right }), { ...query, compareKind: undefined, view: undefined })
  const result = readerIntent(query)
  assert.ok(result?.left && result.right)
  assert.equal(readerKey(result.left), readerKey(left))
  assert.equal(readerKey(result.right), readerKey(right))
  assert.notEqual(readerKey(left), readerKey({ kb: left.kb, id: left.id }))
})

test('fails closed on incomplete comparison and repeated or invalid version selectors', () => {
  const primary = { kb: 'kb-a', doc: 'doc-a' }
  assert.equal(readerIntent({ ...primary, compareKB: 'kb-b' }), undefined)
  assert.equal(readerIntent({ ...primary, compare: 'doc-b', compareVersion: ['one', 'two'] }), undefined)
  assert.equal(readerIntent({ ...primary, version: '../current' }), undefined)
  assert.equal(readerIntent({ ...primary, view: 'foreign' }), undefined)
})

test('opens active correction material only for the matching pinned original', () => {
  const query = { kb: 'kb-a', doc: 'doc-a', version: 'one', compare: 'doc-a', compareKind: 'correction', compareVersion: 'one' }
  assert.equal(readerIntent(query)?.right?.kind, 'correction')
  assert.equal(readerIntent({ ...query, compareVersion: 'two' }), undefined)
  assert.equal(readerIntent({ ...query, compareKB: 'kb-b' }), undefined)
  assert.equal(readerIntent({ ...query, version: null }), undefined)
})

test('groups citations separately from companion rules, attachments and notes', () => {
  assert.equal(relationGroup('cites'), 'references')
  assert.equal(relationGroup('implements'), 'companion')
  assert.equal(relationGroup('template_for'), 'attachments')
  assert.equal(relationGroup('reading_note'), 'reading')
})
