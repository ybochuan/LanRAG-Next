# T4 Native Provenance Implementation Plan

> Execute inline with superpowers:executing-plans and test-driven-development; one fresh-context review at completion. No per-task implementer agents, commits/pushes, live DB migration or production replacement.

**Goal:** Trusted run/step/producer facts and fail-closed model copy reads, with real source identity and inherited governance dependencies.
**Architecture:** Native performs existing authorization, DTO/hash conversion and fixed-loopback calls. Extensions own verification, state, persistence, idempotency and permissions. Unknown inputs cannot seal; unknown copy/metadata/image reads stop before any LLM/VLM consumption.
**Tech:** Existing Go/SQLite/HTTP modules, CGO only for native Lite; no new dependency/root module changes.
**Spec:** `docs/design/M3-T4-Native-Integration-2026-11-01.md`; user explicitly approved the full listed T4 batch, including unknown-source refusal. Human downloads/old grants/T5 excluded.

## Global Constraints

- Preserve007/008/009 and unrelated model-supervisor/UI work. Independent extension module, no imports of core internals or go.work.
- Native calls loopback only, major1,500ms/no proxy/redirect, strict scope/body/hash binding. Provenance service calls reuse server-only AGENTRAG_MANAGEMENT_KEY and per-run capability; neither enters model output.
- No refactoring merge/RRF/Agent/registry/shell/storage/scheduling. Allowed core paths are the spec's table; tests may accompany those paths.
- Missing provider/unknown input/open step/error/expired capability never yields readable copy. Complete is decided by trusted host facts, never a browser/model flag or citations.
- Ready manifest sources retain actual/conservatively inherited model inputs, not permission-only T3 parents. Latest permissions expand/recheck these parents per request.
- Producer binds original run/step/path/final bytes and known prior copy sources; directory collection never assigns provenance. No shrink/owner switching/replay of unknown side effects.
- Keep ordinary user attachments and existing native ACL boundaries; unknown derived workspace/skill/memory/shell/background content is refused as approved. No claim that external copies/downloads can be recalled.
- Work in the existing dirty feature checkout: approved prior changes are dependencies and are preserved. Do not create a clean checkout that discards them.

## Review Focus

- Residual files from another run, identical hashes under different owners, producer ACK unknown: never reassign to the current run.
- Append/edit consumes actual old bytes inside the existing file mutation lock; stale/missing old proof refuses before side effects.
- Tool metadata/images are refused before automatic describeToolImages, not only marked unknown for final seal.
- A new parent dependency between freshness and final transaction is not covered by an earlier allow.
- Default v1 snapshot bytes/root remain compatible; optional component fields fold to their complete parent hash, no partial success.

## Task 0: Prerequisite ledger dependency parity

Files: `extensions/provenance/{permission,policy,manifest}.go`, `extensions/lineage/dependency_ledger_test.go`, `2026-11-01-t4-dependency-guard.md`.
- [x] Original5 negative cases RED (inactive/expired/edited/offline parent/lost declaration head still allowed).
- [x] Shared permission-only closure with original FreshAuthority interface; preflight outside final transaction, final head/closure recheck; actual-source manifest unchanged.
- [x] Current read/admission, actual second writer expands dependencies, derived copy inheritance, wrong-binding zero proof I/O; positive active/review.
- [x] Full build/vet/test0, full isolated Linuxrace0, module/Python0; final prerequisite review found no Critical/Important. New inherited-copy tests included in final rerun.

## Task 1: Persisted run/step/seal/producer facts

Create `extensions/provenance/{run,step,seal,producer}.go` and tests; add next sequential migration010 only after checking current maximum, adjust store prefix/version compatibility tests and readonly CLI known version.
Interfaces: `NewRuns(db, inputVerifier, freshAuthority)`; `Begin(ctx,RunIdentity,operationID) (RunHandle,error)`; `BeginStep(ctx,handle,StepRequest)`, `RecordInput(ctx,handle,InputRequest)`, `FinishStep(ctx,handle,StepResult)`, `Finish(ctx,handle)`, `ReserveProducer/CommitProducer`; `Runs.SealedCopy` implements existing lineage.SealedCopyReader without a core import.
RunIdentity binds scope/user/session/attempt and native initial authorized scope. Run/capability/step/producer IDs are server generated or bound idempotent operations. Store token digests, no prompt/token/raw-file contents. States open/sealed/unattributed/aborted; explicit unknown is monotonic. All step/producer/head/receipt/audit state commits atomically. Facts may reference prior immutable validated copies; empty governed sources cannot publish in the first batch.
- [ ] RED tests:16 seal/CAS contenders1winner; exact retry same receipt; payload/scope/owner/cap mismatch; pending/unknown/aborted/fake empty source denied.
- [ ] Implement using existing single writer and scoped composite FKs. Source observations can be preparatory facts, but run mutation/receipt/audit cannot half-commit.
- [ ] RED→GREEN producer old-run collection, same path/hash wrong step, append inheritance/no shrink, final-byte mismatch, unknown ACK reconcile without reexecution.
- [ ] SQL fault points plus actual child process exits before/after commit, reopen/retry/FK; `go test -mod=readonly ./provenance ./store -count=1` and race.

## Task 2: Current native component proofs and Host service

Native files: existing `extension_source_snapshot.go`, new `extension_source_components.go` plus tests. Extensions: new `provenance/components.go`, `copy_http.go`, Host wiring and tests.
Default GET remains v1. Explicit `view=components-v1` requests1–64 distinct chunk IDs, returns the full snapshot plus complete per-field hash maps for requested chunks. Sort/fold maps identically to v1 fields_hash; no raw content, same authorized read transaction. Unsupported view, missing/foreign/duplicate/over-budget targets reject.
Host endpoint list is fixed in spec. Require version1/service key/explicit configured tenant+KB, bounded strict DTOs and run capabilities. Real Registry/component verifier checks source fields and current active/dependency scope. No production test authority. Resolve exposes only exact-owner copy metadata to Core; check requires freshly native-authorized explicit sources.
- [ ] RED v1 golden compatibility, component fold/current-field mismatch, native scope/read-only/concurrent consistency/limits.
- [ ] RED HTTP key/version/forged scope/complete/cap/body-limit refusal, probe503 and local-ready unchanged.
- [ ] Real HTTP source capture → step facts → sealed proof → existing Ledger admission/read; unknown producer cannot admit. Tests/build in each module separately.

## Task 3: Native run and input DTO plumbing

Files: new `internal/extensionbridge/{source_run,source_copy}.go`; session QA, pipeline governance/into_chat_message/completion{,_stream}, agent {engine,think,act}, factory/governance/search/read/list paths listed in spec.
Use private request context carrying server-issued identity/capability, never tool arguments. Capture actual source field hashes at consumed object boundaries and actual encoded model-call/output hashes. Known formatter/codec composition must be accounted for; unproved transforms mark the run unknown. End run only at actual async execution completion with no open work, not at HTTP return.
- [ ] RED forged client run/empty-source/unknown formatter refused; native scope narrowed; active raw text positive and parent/neighbor/FAQ/image unknown cannot complete.
- [ ] RED parallel steps, source change between load/send/seal, tool errors, cancellation/pending work, repeated model calls preserve union and do not replay side effects.
- [ ] Implement transport/DTO only; component/proof rules remain Host-owned. No new permission cache.
- [ ] RAG/Agent original active capabilities and fail-closed tests, targeted package tests.

## Task 4: Actual file producers and copy read gates

Files: artifact_collector/agent_artifact_files, sandbox_write/sandbox_edit, read_file/read_artifact/workspace_reader/sandbox_ls and governance helpers.
Reserve producer tied to actual successful write step; authorize old bytes and inherit known copy before append/edit within original lock. Compare intended final content with actual stored hash; producer/copy logical heads are Host facts. Collector can only resolve an existing matching producer; never assign old files to current run.
Read target native ACL first; resolve exact binding → native source ACL → Host current check. Read/hash actual data and check again before exposure. Cached pages/images use same bound whole-byte hash/current check. Plaintext unknown file names/paths cannot leak via directory metadata.
- [ ] RED active source→model→write→collect→cross-turn read positive; stale/version/offline/503/missing producer negative.
- [ ] RED residual file, wrong owner/resource/session/path/step/hash, same mtime different bytes, old capability, producer ACK lost, append source revoked, no shrinking.
- [ ] Keep deterministic algorithm tests via explicit test-only dependency fixtures; default production constructors remain deny on missing policy. Never weaken native ACL assertions or install test permits in production.
- [ ] Actual restart/real SQLite/Host/Core upload plus read tests; source/raw output markers independent.

## Task 5: Unknown-copy side channels

Files: shell_exec{,_async}, work, memory_recall/search_memory/sessionAgentQA, background, web_fetch/read_web_page, engine metadata/factory paths from spec.
- [ ] RED no-source shell refuses before command starts; existing jobs keep status/cancel but omit unknown detailed payload/title/output paths.
- [ ] RED automatic/explicit memory, skill metadata, directory names, cached web/old URL and notices cannot reach ANY model input. Do not ack unseen notice text or delete stored data.
- [ ] RED unknown tool images never invoke automatic VLM; guard result before describeToolImages. Pure metadata control remains typed and scoped.
- [ ] Implement thin refusal/DTO hooks only; do not patch schedulers, registry, memory storage or underlying file algorithms.

## Task 6: Complete verification and delivery

- [ ] All extension build/test/vet/format, independent boundary/Python, full isolated Linuxrace; affected native tests/build; frontend only if changed.
- [ ] Real upload/Observer/version/run/model/producer/copy read→revoke→restart→deny E2E. Capture body/title/description/file-name/image markers at all model/VLM/rerank requests; deterministic fixture for failures, model-supervisor real model positive smoke when credentials/reachability available.
- [ ] One final fresh-context whole-T4 review, one fixes pass with RED→GREEN and full rerun. Update PATCHES/context/backlog/evidence and scope check. M2/T4 stay incomplete until every required gate passes; T5 remains separate.

## Ledger

- User approved entire T4 source/copy thin-hook list and unknown-source safe refusal; no further per-file approval within that list. Added files or business/algorithm changes require report first.
- Source design builder check: producer attribution and metadata/VLM ordering fixed in spec; read-only review did not change core.
- Built-in model-supervisor is running on127.0.0.1:8092 (`/healthz`200); current process/root.env has no supervisor token. No token printed/changed, no model process manually started. Pure DB checks do not need a model; real model smoke will use the built-in supervisor.
- 2026-11-02 resumed: installed requested Stop That Shit0.2.5 and followed its direct-solution scope guidance. Native QA/think/act/completion consume real run and step APIs; actual text/header fields are checked by existing Host component authority. Real Core/Host synthetic-model E2E now requires a sealed native execution, input facts, FK integrity, stale/offline refusal and no terminal open runs. File producer/collector/known-copy restoration are still pending; Tasks3–6 are not marked fully complete.
- Ruling: unadapted rewrite/rerank/FAQ/images/merged text/custom metadata and listing updated_at stay unattributed — the approved spec permits conservative refusal without changing native algorithms — positive copy availability remains limited until those actual call paths are adapted. Opaque MCP catalog/pin metadata is omitted before model consumption under the approved unknown-source boundary.
