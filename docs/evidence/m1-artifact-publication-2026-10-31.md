# M1 artifact publication barriers and crash windows — 2026-10-31

Scope: user requested continued M1 progress. This slice advances M1-05P without native modifications, dependency changes, schema changes, physical shutdowns or destructive storage tests. Existing dirty/untracked M1 work preserved. No commits or pushes.

Workspace D:/LanRAG-Next; HEAD fa6353e8c821daa5eeb667dc46140e4d0d32f179; describe v0.9.0-2-gfa6353e. Plan: docs/superpowers/plans/2026-10-31-m1-artifact-publication.md.

## Production files and behavior

| File | Change |
|---|---|
| extensions/parser/artifacts.go | File Sync → staging-directory Sync → whole-directory Rename → artifact-root Sync. Any barrier failure precedes SQL registration/ZIP success. Existing identical archive is verified and its document/root directories are resynced before retry success. |
| extensions/parser/artifact_sync.go | Non-Windows os.Root directory open, File.Sync and Close; errors propagated. No promise about hardware or directory ancestors created outside this operation. |
| extensions/parser/artifact_sync_windows.go | Preserve previous Windows file Sync + rename. Deliberately no POSIX-equivalent directory flush or power-loss claim. |

The per-instance syncDirectory override is unexported, only configured in package fault tests before use, and cannot be supplied by an HTTP caller. Production uses the platform function.

## Tests and observed failures

New files: artifact_sync_test.go and artifact_crash_test.go.

Initial targeted test command exited1 because artifactStore had no syncDirectory field. After minimal implementation, directory-order/retry and injected-error tests exited0 on Windows and Ubuntu. Nine behavioral scenarios cover initial/retry order, two direct failures, two HTTP/SQL failures and four actual process-exit windows. The helper test skips unless launched as a child process.

| Scenario | Assertions |
|---|---|
| Initial publication | Both original files exist before staging sync; final doc absent until rename; root sync after final doc exists. |
| Existing identical archive | Repeat document-directory and root-directory barriers; do not skip a previous failed publication barrier. |
| Stage sync failure | No final document publication, persist error propagated. HTTP503, SQL artifact count0. |
| Root sync failure after rename | Original final files remain intact, persist error propagated. HTTP503, SQL artifact count0. Retry repairs barrier, registers once; formal parse count remains1. |
| Crash after staging sync | No final document/SQL row; orphan pending data remains. Recovery publishes/registers using existing job, leaving old pending data untouched. |
| Crash after rename before root sync | Final file present but SQL count0. Recovery verifies and resyncs, then registers; does not recreate job. |
| Crash after root sync before SQL | Final file present but SQL count0; recovery registers existing original bytes. |
| Crash after SQL commit before HTTP ACK | Child exits from a response writer before returning success headers; SQL count1 survives. Restored output capability returns same archive with exactly one registration/job. |

All crash tests execute the test binary in a real child and call os.Exit(23), bypassing deferred cleanup/DB Close. They use the actual native Handler, artifactStore and parserJournal paths. After recovery they compare disk bytes with SQL BLOB, run foreign_key_check, require remote formal job count1, retain old pending data and reject cross-document access with403. They do not emulate dropped disk writes, physical power cuts or VM termination.

## Executed checks

Go commands below run in extensions/. Windows CGO_ENABLED=0; GOMOD points to extensions/go.mod, GOWORK empty.

| Command | Exit | Result |
|---|---:|---|
| go build -mod=readonly ./... | 0 | extension build/type check |
| go test -mod=readonly ./... -count=1 | 0 | all13 test-bearing packages pass; parser-calibrate command has no standalone command tests |
| go vet -mod=readonly ./... | 0 | no diagnostics |
| gofmt -l . | 0 | no output |
| python scripts/check_module_boundary.py | 0 | 308 imports, no WeKnora dependency/workspace |
| go list -deps -f '{{.ImportPath}}' ./... | 0 | explicit dependency output scanned: no WeKnora internal import |
| python -m unittest discover -s scripts -p '*_test.py' -v | 0 | 26 tests, skip1 for Linux process-group lifecycle on Windows |
| Ubuntu CGO_ENABLED=0 go test -mod=readonly ./parser -run 'TestArtifactDirectory\|TestArtifactPersists' -count=1 | 0 | initial real directory sync target run |
| Ubuntu CGO_ENABLED=1 go test -mod=readonly -race ./... -count=1 | 1 | only existing unchanged Outbox assertion fails; parser/other packages pass, no new race diagnostic |
| Ubuntu CGO_ENABLED=0 go test -mod=readonly ./parser -run 'TestArtifact(Directory\|Barrier\|Publication)' -count=1 -v with TMPDIR on owned /mnt/d/LanRAG-Next/tmp/m1-artifact-disk-* | 0 | extra target-storage run, not just default tmpfs |
| python -B extensions/scripts/m1_native_e2e.py --core /mnt/d/LanRAG-Next/tmp/m1-native-build-83iqcjjt/WeKnora-lite --joint-m0 --report tmp/m1-artifact-publication-e2e.log | 0 | actual core/Host/MinerU combined test, full output below |
| Independent review targeted artifact tests, git diff --check | 0 | Critical0 / Important0 / Minor0; no fix pass or second review |

Ubuntu commands use `wsl -d Ubuntu --exec /usr/bin/env PATH=/usr/local/go/bin:/usr/bin:/bin GOWORK=off ...`; no reliance on the default WSL distro. Final module/range and unchanged root-file hashes were also checked. No root full Go suite/frontend build or new core build in this extension-only slice. The actual prior CGO core binary is unchanged; E2E builds a fresh Linux Host with CGO disabled.

## Storage and remaining acceptance

Observed `stat -f`: /tmp is tmpfs, /mnt/d is v9fs. Default Linux tests alone therefore do not supply disk-backed evidence. The extra target-storage test sets TMPDIR to a newly owned directory under repository tmp, checks its resolved location remains under that root, runs the targeted suite and removes only that owned test directory. All four child-exit windows pass there too. Actual E2E also invokes the Linux directory barrier on the test-owned Windows-backed temporary path.

Successful directory sync syscalls on v9fs are not proof that the Windows host, VM and device honor every flush through physical power loss. Windows builds do not implement the new directory flush. The operation also does not cover creation durability of the artifact root/its ancestors. These limits keep M1-05P physical power-loss acceptance open. Old pending directories are not auto-deleted/adopted; absent job history for an existing archive remains fail-closed.

Real labeled corpus is still absent; image/complex preflight, OCR/other tiers, fullUnicode/garble detection and real per-KB calibration remain incomplete. No invented metrics or OCR download attempt. No cloud model calls in this slice; local deterministic model fixture supplies the joint M0 answer check.

## Full Linux race result (retained failure)

```text
ok  github.com/ybochuan/LanRAG-Next/extensions/bridge 11.960s
ok  github.com/ybochuan/LanRAG-Next/extensions/cmd/extension-host 1.016s
?   github.com/ybochuan/LanRAG-Next/extensions/cmd/parser-calibrate [no test files]
ok  github.com/ybochuan/LanRAG-Next/extensions/contract 1.160s
ok  github.com/ybochuan/LanRAG-Next/extensions/host 1.215s
ok  github.com/ybochuan/LanRAG-Next/extensions/ingest 1.218s
ok  github.com/ybochuan/LanRAG-Next/extensions/integration 1.372s
ok  github.com/ybochuan/LanRAG-Next/extensions/lifecycle 1.309s
--- FAIL: TestRunRetriesReportsAndDrainsHealthyBacklog (0.06s)
    worker_test.go:102: calls=3 reports=2
FAIL
FAIL github.com/ybochuan/LanRAG-Next/extensions/outbox 0.652s
ok  github.com/ybochuan/LanRAG-Next/extensions/parser 4.952s
ok  github.com/ybochuan/LanRAG-Next/extensions/retrieval 1.013s
ok  github.com/ybochuan/LanRAG-Next/extensions/store 1.845s
ok  github.com/ybochuan/LanRAG-Next/extensions/taggov 1.356s
ok  github.com/ybochuan/LanRAG-Next/extensions/weknora 1.297s
FAIL
```

This existing Outbox timing assertion is neither fixed nor masked by a rerun. It is separate from the new publication tests and is not a new DATA RACE report.

## Full actual combined E2E output

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=0 normalized_marker_chunks=1 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[SQL] PASS: original middle JSON byte-for-byte; schema/MinerU versions; known formal job FK; no foreign key violations
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks
[JOINT-OFFLINE] HTTP=200 answer_generation_calls=0 correlated_gate=True
[JOINT-OFFLINE] PASS: correlated native before_retrieve refusal; no generated answer
[RECOVERY] PASS: restarted production Host restored verified source capability and original job receipt; actual MinerU POST count remains one
[JOINT-ONLINE] HTTP=200 answer_generation_calls=1 correlated_gate=False
[JOINT-ONLINE] PASS: same JWT/session/query/KB/doc; restarted parser-enabled Host received gate; terminal generated answer
[RAG] Live models not exercised; joint mode uses a deterministic local answer fixture
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESOURCES] PASS: zero audited Python external attempts; zero private ONNX weights
[RESULT] PASS
```
