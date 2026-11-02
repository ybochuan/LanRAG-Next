import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

/** Test-only POSIX shell; Windows uses Git's shell instead of a WSL launcher. */
export function posixShell(name: 'sh' | 'bash'): string {
  if (process.platform !== 'win32') return name === 'sh' ? '/bin/sh' : 'bash'

  const git = spawnSync('git', ['--exec-path'], { encoding: 'utf8', timeout: 5_000 })
  if (git.error || git.status !== 0 || !git.stdout.trim()) {
    throw new Error('POSIX shell tests require Git for Windows on PATH')
  }
  const binary = resolve(git.stdout.trim(), '../../..', 'usr', 'bin', `${name}.exe`)
  if (!existsSync(binary)) {
    throw new Error('POSIX shell tests require the Git for Windows usr/bin shells')
  }
  return binary
}
