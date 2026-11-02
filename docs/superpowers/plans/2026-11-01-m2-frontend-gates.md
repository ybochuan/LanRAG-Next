# M2 frontend acceptance gates

**Goal:** Clear the eight reproduced Windows frontend baseline failures while preserving behavioral assertions and existing product code.

**Method:** Inline execution using systematic-debugging, writing-plans and executing-plans; one independent review at the end. User requested continuation of M2 work. No commits, core changes, dependency upgrades or live-data changes.

## Evidence and scope

- HEAD fa6353e, v0.9.0 baseline; existing dirty work preserved. Fresh six-file run exits1 with the same eight failures, log `tmp/m2-frontend-gates-red.log`.
- Three Network drawer assertions, PDF failure assertion and Skill status assertion fail on CRLF; read-only experiment makes each match using LF normalization.
- PPTX alias is the installed ESM renderer, but its Windows backslashes do not match the Unix-path assertion. Keep actual esbuild runtime-deduplication checks.
- Windows `bash` resolves to a WSL shim whose distro lacks /bin/bash; `/bin/sh` is not a Windows executable. Installed Git for Windows provides real POSIX shells. Do not skip shell behavior or change the production scripts.
- M2 supplement propagation remains blocked on authoritative M3 lineage. Do not invent relationship facts or widen the native patch to clear this gate.

## Tasks

- [x] Normalize CRLF to LF only when reading source in `SandboxConfigEditorDrawer.network.test.mjs`, `pdfjsWasmAssets.test.mjs`, `SkillSettings.test.mjs`. Run the three tests; existing secret, deny-all, reorder, PDF-error and served-version checks remain unchanged.
- [x] Normalize filesystem separators in `src/utils/pptxRuntime.test.ts` assertions, preserve entry suffix and actual esbuild one-runtime/no-CJS checks. Run it separately.
- [x] Add test-only `src/test-utils/posixShell.ts`: `posixShell(name: 'sh'|'bash'): string`, POSIX defaults on non-Windows and Git-for-Windows shell resolved from `git --exec-path` on Windows. Fail with a clear error if missing; no silent WSL fallback/skip.
- [x] Update `SandboxTerminal.test.mjs` and `cliIntegration.test.ts` to use the shell helper. Source the PTY script with a positional parameter; verify actual aliases and unchanged hostile-host literal behavior. Add a real fixture-path test containing spaces/apostrophe if needed to validate forwarding.
- [x] Run six-file regression, full `npm test`, `npm run type-check`, `npm run build`. Review test mutations to prove removed security/render failures still fail. Core/Host recompile and redeploy are unnecessary for test-only changes.
- [x] Independent scoped review, update validation record and backlog. Run git diff/check/status and verify dependencies/core/product source unchanged from task start.

## Ledger

- Ruling: Do not reconfirm the already selected inline execution method; the continuation authorizes fixing the existing M2 frontend gate. No new feature or state-transition design is introduced.
- Ruling: Normalize test inputs, not all repository line endings; costs limited test-harness adaptation while retaining the original business expectations.

- Delivered: all8 failures removed, full suite2369/pass2368/fail0/skip1; typecheck/build0. Three copied-source mutations rejected. Independent review0 findings; final affected tests34/34. Only changed test lines useLF to pass literal diff-check while preserving existing endings. Product/dependency/runtime fingerprints unchanged; native backend and M3-dependent supplement tasks remain open.
