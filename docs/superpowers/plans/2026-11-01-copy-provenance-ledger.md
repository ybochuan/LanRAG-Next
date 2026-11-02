# Approved extension-only copy provenance ledger

User approved2026-11-01: synchronous source ledger and offline concurrency/fault validation inside extensions; native integration requires separate approval. Existing tools/downloads remain unchanged. Inline execution; one independent review at end. Existing dirty work preserved, no commits/pushes/live DB migration.

## Interfaces and trust boundary

`extensions/provenance`: `New(db *store.DB, authority Authority) *Ledger`; `RegisterSource(ctx, SourceRequest) (Receipt,error)`; `Save(ctx, SaveRequest) (Receipt,error)`; `CheckRead(ctx, ReadRequest) (Decision,error)`; `Snapshot(ctx, Scope, copyID) (Manifest,error)`.

Scope is tenant+KB; Binding is server-derived user/session/resource/content-version/SHA256. Authority supplies current document version proofs and sealed complete copy proofs. No HTTP route, user-supplied complete flag or production fake authority. Nil authority allows pending drafts only. Current native document-version proof is absent until M3/native adapter; do not fabricate it.

Sources are immutable version records anchored to scoped lifecycle event FK; current source heads use revision CAS and monotonic native sequence. Copy revisions are immutable, current head CAS; receipt hash binds operation kind/scope/payload, exact retry returns historical receipt. A receipt is not a read permit.

Ready copies require verified sealed proof, current source versions, current active lifecycle, same-scope parents and complete transitive union. Updating a copy cannot change its owner or shrink its source set. Current read repeats binding/version/lifecycle/expiry checks in one DB snapshot; pending, unknown, malformed or unavailable always deny. Parents are prior immutable versions, graph references cannot point into the future. No cross-KB/tenant support.

## Steps

- [x] RED behavioral tests for draft denial, trusted admission, active→stale denial, version change, tenant/owner/hash mismatch, replay/conflict/CAS, multi-source inheritance/no shrinking.
- [x] Migration006: source versions/heads, manifest versions/heads, normalized source/parent references, receipts and audit with composite scope FKs; integrate automatic/manual-prefix migrations and update version expectations.
- [x] Implement independent source ledger, proof validation and read-only decision. No imports of native internal, no root dependencies or Host endpoint.
- [x] Real SQLite concurrency:16 identical commands one mutation;16 CAS writers one winner, including two independent DB writers; conflicting operation payload rejected; closed DB fail-closed.
- [x] Inject SQL failures between manifest/sources/parents/head/receipt/audit and prove rollback; child os.Exit during transaction/after commit and reopen/replay checks. Validate upgrade/reopen/FKs.
- [x] Complete extension build/test/vet/format, Python/module boundary, Linux race; one review and fixes, record honest limits and exact outcomes. Native baseline audit results are independent, no product fixes in this scope.

## Ledger

- Approval: C-27 synchronous authority approved; derived_from remains async and is not used as permission truth.
- Ruling: CurrentVersion/SealedCopy are trusted adapter ports, not authentication from arbitrary callers. Offline fixtures implement them explicitly; live Host is not wired, so no runtime coverage claimed.
- Ruling: Source-head revision and native sequence fence old evidence. No inferred DocID-as-version; lacking proof stays pending/denied.
- Ruling: Lifecycle event is an immutable scoped version anchor, not necessarily the latest lifecycle event. Current lifecycle and effective dates are checked separately on admission/read; lifecycle transitions do not invent new document versions.
- RED→GREEN: ancestor correction must invalidate old descendants; prospective current-head cycles cannot commit a ready manifest; failed transactions return zero receipt. Each behavior first reproduced and then fixed.
- Integration RED→GREEN: schema6 broke the read-only review CLI's exact version5 gate; explicit5/6 acceptance now preserves queries, schema7 remains rejected, and DB bytes remain unchanged. OpenReadOnly is unchanged.
  - **注（2026-11-01 追加，晚于本计划）**：schema7 已被 `extensions/store/migrations/007_lifecycle_current_consistency.sql` 的生命周期一致性守卫占用，只读 CLI 的版本门已放宽为接受 schema5–7、仍然拒绝更高版本。上一条的「schema7 remains rejected」只描述当时那次改动的实现状态，不是当前事实；当前事实见 `../../evidence/m2-e2e-mirrored-2026-11-01.md` 第 10 节。
- Review: one independent extension implementation review found no Critical/Important; narrow CLI follow-up also clear. Fresh targeted tests exit0.
- Final verification: full extension build/test/vet and Linux race exit0;312-import boundary gate exit0; Python33 tests with3 skips exit0; gofmt no output. Evidence: `../../evidence/copy-provenance-ledger-2026-11-01.md`.
- Native baseline: complete two-package rerun exit1,49 top-level failed tests; no fixes or complete baseline attribution claimed. No native/Host wiring, live DB upgrade, service restart, commit or push.
