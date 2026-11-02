# M1 recovery, artifacts and calibration follow-up — 2026-10-31

This slice implements the user's request to handle unfinished M1 work. Workspace D:/LanRAG-Next; branch codex/m1-parser-routing; HEAD fa6353e8c821daa5eeb667dc46140e4d0d32f179, describe v0.9.0-2-gfa6353e. Existing uncommitted M1 work preserved. No commits/pushes. No new core production changes in this slice.

## Delivered and validation

| Item | Files | Current evidence |
|---|---|---|
| Durable job intents and document capabilities | extensions/parser/recovery.go, native.go, scope.go, mineru_jobs.go, mineru_upload.go; store/migrations/003_parser.sql | Intent before remote POST; canonical physically verified content + trusted scope/document + effective policy + upstream key. Changed backend file IDs reuse a job. Known receipts restore; unknown replies never resubmit. |
| Safe capacity | parser/native.go, recovery_test.go | 130 complete upload→preflight→formal job→ZIP archive cycles; restored earliest job; 12 concurrent duplicate calls create one job; active formal job remains resident while idle scopes are evicted. |
| Actual SQLite concurrent writers | parser/recovery_test.go | 12 claims across two independently opened SQLite writer connections produce exactly one committed intent; losers are uniqueness conflicts, no remote side effects. |
| SQL artifact registry | parser/artifacts.go, mineru_proxy.go, recovery.go; store/migrations/003_parser.sql | Original middle JSON bytes, format/schema/MinerU version; formal-job and tenant/KB/doc FK; existing identical FS publication repairs missing SQL registration after failure. Failed registration returns503 before ZIP ACK. |
| Recovery faults | parser/recovery_test.go | Real child test executable os.Exit(23), bypassing all defers, after intent/before POST, after receipt and after archive. No fake graceful shutdown used for these assertions. DB closed before formal request sends zero POSTs; unknown response + restart + changed file ID sends no second POST. |
| Database upgrade/durability | store/sqlite.go, durability_test.go, migrate.go, migration_test.go; host/runtime.go | Separate WAL/FULL writer when parser enabled; v1/v2→v3 preserves existing rows; default M0 Open remains NORMAL. Host probe+parser owns artifactRoot/.parser-state.sqlite; worker+parser shares its extension DB. |
| Calibration tool | parser/calibrate.go, calibrate_test.go, cmd/parser-calibrate/main.go, testdata/parser/manifest.json | Actual PDF/hash validation, held-out metrics per tenant/KB; rejects empty corpus, duplicate bytes/document family, wrong hash/path. No automatic threshold fitting. User confirms no real corpus/labels; empty manifest intentionally fails. Synthetic fixtures validate measurement only. |
| Real combined E2E | scripts/m1_native_e2e.py, m1_native_e2e_linux.py, m1_native_e2e_test.py | Actual WSL CGO core, production Host and installed Windows MinerU4.0.1; native upload, SQL exact bytes/FK, Host restart original job, M0 correlated offline refusal + online generated answer. Local deterministic model fixture; this slice does not rerun DeepSeek. |

## Final verification commands

All Go extension commands run in D:/LanRAG-Next/extensions. Linux commands explicitly use Ubuntu, not the default WSL distro.

| Command | Final exit | Detail |
|---|---:|---|
| go env GOMOD GOWORK | 0 | extensions/go.mod; GOWORK empty |
| go build -mod=readonly ./... | 0 | Windows CGO disabled; actual E2E also builds Linux Host with CGO_ENABLED=0 |
| go test -mod=readonly ./... -count=1 | 0 | 13 packages pass; new parser-calibrate command has no standalone command tests; its library and Python integration acceptance are tested |
| go vet -mod=readonly ./... | 0 | no diagnostics |
| gofmt -l . | 0 | no output |
| python scripts/check_module_boundary.py | 0 | 308 imports; no WeKnora dependency or active workspace |
| python -m unittest discover -s scripts -p '*_test.py' -v | 0 | 26 tests, 1 skip for WSL/Linux process-group lifecycle on Windows |
| wsl -d Ubuntu --exec /usr/bin/env PATH=/usr/local/go/bin:/usr/bin:/bin GOWORK=off CGO_ENABLED=1 /bin/bash -c 'cd /mnt/d/LanRAG-Next/extensions && go test -mod=readonly -race ./... -count=1' | 0 | final full race run passed every package; earlier failures retained below |
| go run -mod=readonly ./cmd/parser-calibrate | expected nonzero | empty corpus emits `version 1 nonempty labeled corpus required`, executable exit2, go run reports exit status2; not real calibration acceptance |
| python -B extensions/scripts/m1_native_e2e.py --core /mnt/d/LanRAG-Next/tmp/m1-native-build-83iqcjjt/WeKnora-lite --joint-m0 --report tmp/m1-recovery-joint-e2e-reviewed.log | 0 | full final terminal output below |

No root full Go suite, frontend rebuild or physical power-cut test run in this extension-only slice. The existing actual CGO core binary from the prior integration is reused; no claim of a new root binary build. Previous root service/SSRF environment failures remain separately recorded in m1-native-integration-2026-10-31.md.

## RED → GREEN and single review

Initial recovery tests failed because NewPersistentNativeHandler did not exist and migration remained version2. Initial calibration tests failed due missing API. After implementation both passed, followed by full suite.

One fresh independent whole-slice review: Critical0 / Important3 / Minor0. One fix pass, each with observed regression failure then success:

1. DSN string replacement could rewrite uppercase NORMAL in an existing database path and leave writer synchronous=NORMAL while reporting FULL. Test observed `writer synchronous=1 want FULL` and unintended FULL/state.db. Fixed by configuring the synchronous pragma explicitly, preserving the path; regression and full suite pass.
2. Canceled GET poll poisoned the document when its snapshot used the canceled request context, blocking the native client's detached DELETE. Test observed HTTP503 and remote cancels=0. State commit now uses an independent bounded5s context; formal create still requires caller-context intent. Regression confirms controlled cancel HTTP200 and remote cancels=1.
3. Existing pre-journal archive with no snapshot was treated as a new document. Test observed silent HTTP200 adoption. Restore now rejects existing archives without verified job history before a new parse side effect; fresh other documents still work. No automatic archive adoption/deletion.

No second review loop; no deferred minors from this review.

## Retained failures and limits

- Standalone old M0 script first exited1 because its bind preflight reported8080 unavailable immediately after a previous test. No unknown process stopped; no conclusion about a listener owner inferred. Enhanced combined E2E later used core8080/Host8090 successfully and proved both phases.
- First full Linux race attempt exited1: known Outbox TestRunRetriesReportsAndDrainsHealthyBacklog `calls=3 reports=2`, plus a new fakeMinerU fixture initialization race exposed by subprocess HTTP. Fixed test fixture to assign an unstarted httptest server before Start.
- Second full Linux race attempt exited1 only for the same existing Outbox assertion; parser/store/host/bridge passed. Final fresh full run exited0, including Outbox, without modifying its implementation/test. This is not evidence that the baseline flake was fixed.
- Real corpus/labels absent by explicit user reply. No 85%/10%/15% real metric or threshold fitting acceptance. Image-file preflight, full Unicode/garble/layout detection, actual OCR resources/other tiers remain incomplete M1 scope; no synthetic fixture replaces these gates.
- Unknown remote accepted-but-unconfirmed job IDs cannot safely be correlated using native MinerU V1. Return503/manual reconciliation, never blind POST replay. No exactly-once assertion.
- Existing archive without journal requires manual recovery; changing backend or artifact root is not a transparent migration. Same-document revisions/changed policies cannot overwrite old artifacts; per-document128 upload limit remains.
- File publication and SQLite registration form a recovery protocol, not a single transaction across resources. SQLite FULL/file Sync do not prove directory metadata/device power-loss behavior. Real power-cut and deployment filesystem recovery remain M1-05P; no recursive cleanup of pending directories.
- Production Host trusts authenticated core identity plus configured tenant/KB; no new native ownership lookup added. Native algorithms, root module dependencies, Frozen Architecture and baseline files untouched.

## Final actual combined E2E terminal output

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
