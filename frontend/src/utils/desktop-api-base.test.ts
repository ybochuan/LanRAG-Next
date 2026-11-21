import assert from 'node:assert/strict'
import test from 'node:test'
import { desktopOriginFromApiBase } from './desktop-api-base'

test('desktop upload origin accepts an injected http api base', () => {
  assert.equal(desktopOriginFromApiBase('http://127.0.0.1:60381/api/v1'), 'http://127.0.0.1:60381')
  assert.equal(desktopOriginFromApiBase('  https://example.test/api/v1/  '), 'https://example.test')
})

test('desktop upload origin rejects missing or unusable values', () => {
  assert.equal(desktopOriginFromApiBase(undefined), null)
  assert.equal(desktopOriginFromApiBase(''), null)
  assert.equal(desktopOriginFromApiBase('/api/v1'), null)
  assert.equal(desktopOriginFromApiBase('file:///tmp/api'), null)
  assert.equal(desktopOriginFromApiBase('not a url'), null)
})
