# M3 Trusted Sources T1/T2 Implementation Plan

> Inline execution using superpowers:executing-plans; one final independent review. No commits/pushes, live DB migration or service replacement.

**Goal:** Trusted current native snapshot fingerprints and extension-owned immutable observed versions; do not claim complete run provenance or M2 acceptance yet.
**Architecture:** Existing native authorization → one read transaction → hashes only. Extension reader validates fixed DTO; the shared SQLite writer serializes acquisition through registration. Fresh verification precedes copy read permission.
**Tech:** Existing Go/GORM/SQLite/HTTP only; no root/module dependencies.
**Spec:** `docs/design/M3-Trusted-Sources-2026-11-01.md`; user explicitly approved T1/T2.

## Constraints and rulings

- Continue the user's existing `codex/m1-parser-routing` checkout and retain its uncommitted M2 dependencies; no clean/reset/stash/branch integration. Other ongoing model-supervisor/schema7 work is outside this plan and preserved.
- Native production scope: knowledge handler DB injection, new readonly snapshot handler, one route. No native table/algorithm writes.
- Next migration is008; preserve published001–007 and their facts. Unattested schema6 evidence is never promoted automatically.
- ObservationSequence means extension observation order, not native monotonic generation. Normal edits do not imply superseded/Revision Published.
- Limits:4096 chunks,64MiB hashing input,2MiB response,500ms native snapshot deadline. Completed/stable index only, never truncated success.
- Review focus: concurrent native edits, complete canonical fingerprints, scope/soft deletion, cross-process stale-response ordering, preservation of existing source references and audits.

## Task1: Native current snapshot

Files: `internal/handler/knowledge.go`, new `extension_source_snapshot{,_test}.go`, `internal/router/routes_knowledge.go`.

- [ ] Write tests for deterministic fingerprints, content/header/metadata/revision changes, foreign scope, pending/indexing/empty/overflow rejection, HTTP auth/unknown scope/no raw text and no writes.
- [x] Run `CGO_ENABLED=1 go test -mod=readonly -tags sqlite_fts5 ./internal/handler -run '^TestSourceSnapshot' -count=1`; missing feature RED observed, then GREEN including concurrent SQLite writer/consistent old snapshot.
- [ ] Implement SourceSnapshot DTO and hash whitelist in native handler; same transaction queries all live chunk types; PostgreSQL repeatable-read, SQLite consistent read. Inject existing *gorm.DB, add existing authorized kbRead route.
- [ ] Verify targeted tests, real concurrent SQLite transaction fixture and compilation. Preserve root go.mod/go.sum.

## Task2: Extension observed-version authority

Files: new `extensions/lineage/{snapshot,versions,authority,edges,query}.go` and behavioral tests; new `store/migrations/008_lineage_sources.sql`; migration/manual-prefix and version-sensitive tests/readonly CLI; provenance types/read checks.

- [ ] RED: actual source reader validates recomputable root, complete scoped leaves, bounded responses; fixed origin/no redirects. Register snapshots into family/version/receipt/audit; same-root replay, changed payload conflict and new-root next observation.
- [ ] Implement composite scoped FKs, immutable nodes/versions/evidence, CAS family/current head and receipts/audit in one writer transaction. Fetch inside writer to serialize acquisition across independent DB instances; failed request rolls back and retry recollects.
- [ ] Add FreshAuthority contract: force current-root verification before read, then compare authority head again inside final DB view. Existing fake-only source facts retain bytes/history but cannot satisfy real evidence lookup.
- [ ] Implement split lineage query edge allowlists and bounded cycle guard; do not create automatic supersedes or lifecycle changes for ordinary snapshots.
- [ ] Test16 concurrent same-root refreshes,2 independent writers and blocked fetch ordering; SQL failure rollback, commit-before-ACK replay, schema6/7 upgrade preservation and FK integrity.
- [ ] Full extension build/test/vet/race, module boundary/Python; native affected-package tests and Lite candidate build; one independent review and scoped fixes.

## Progress / evidence

- Earlier native baseline:49 top-level failures isolated and resolved through3 test-only fixtures + WSL python3.14-venv prerequisite. Fresh full tools/service regression exited0 (both packages); production guards unchanged.
- Frontend full test/type/build completed before latest concurrent work; retain logs but rerun final gate against final inputs before global claims.
- T1/T2 not yet complete; T3 supplement/Host integration, T4 sealed actual inputs and T5 streaming revocation require their concrete subsequent plan. M2 stays unaccepted until remaining gates pass.
- Native candidate `tmp/m2-m3-WeKnora-lite` build exit0; no installation or live service restart.
- Targeted lineage/provenance/store/retrieval/lifecycle/readonly CLI tests and Linux race exit0. Shared contract at `contracts/bridge/v1/native-source-snapshot.json`, synthetic data only.
- Real owned Core/worker Host E2E via `extensions/scripts/m3_source_e2e.py` exit0: upload/chunks/Observer active, native HTTP snapshot→version→source ledger; native chunk edit changes current root and denies old evidence; normal edit preserves active. Full existing UI date/CAS/policy/auth tests also pass. Complete sealed-run authority remains absent/denied, not claimed as delivered.
- Ruling C-37: schema7 write triggers do not retroactively reject malformed existing active projections. Reproduced lagging head and state mismatch permitting retrieval/copy after reopen; added shared extension read integrity check and RED→GREEN tests. Invalid projections are not treated as absent/initialized.007 unchanged (SHA256125B9261D2A8D00DB7D560511514BF57385EEBCE5EFA95397C14556506993663);008 contents match this plan's original table definitions.
- Ruling: preserve legacy schema6 source sequence as a bookkeeping lower bound when allocating first observed version; no legacy proof is promoted. Test first reproduced fresh registration conflict, now freshly verified evidence can replace old head without deleting facts.
- Compatibility: strict manual-prefix adoption includes008; readonly CLI explicitly accepts5–8, unknown9 rejects and DB bytes remain unchanged. Existing schema7 guards preserved.
