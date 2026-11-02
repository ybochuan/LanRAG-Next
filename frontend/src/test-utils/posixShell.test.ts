import assert from 'node:assert/strict'
import test from 'node:test'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { posixShell } from './posixShell'

test('test shell sources paths with spaces and apostrophes using a positional argument', () => {
  const directory = mkdtempSync(join(realpathSync(tmpdir()), "agentrag shell's "))
  try {
    const fixture = join(directory, "prompt's fixture.sh")
    writeFileSync(fixture, "printf '%s' 'fixture sourced'\n")
    const out = execFileSync(posixShell('bash'), [
      '--norc', '--noprofile', '-c', '. "$1"', 'source-fixture', fixture.replace(/\\/g, '/'),
    ], { encoding: 'utf8', timeout: 10_000 })
    assert.equal(out, 'fixture sourced')
  } finally {
    // Delete only the uniquely created fixture directory under the known temp root.
    assert.equal(dirname(resolve(directory)), realpathSync(tmpdir()))
    rmSync(directory, { recursive: true, force: true })
  }
})
