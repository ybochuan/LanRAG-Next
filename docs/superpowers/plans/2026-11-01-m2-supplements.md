# M2 Supplement Integration Implementation Plan

> Inline execution with superpowers:executing-plans; TDD per behavior and one independent final review. No commits/pushes/live migration or service replacement.

**Goal:** Explicit same-KB supplemental dependencies, atomic critical relation facts and inherited validity/review with integrated native management UI.
**Spec:** `docs/design/M2-Supplement-Integration-2026-11-01.md`, explicitly approved by user after T1/T2.
**Architecture:** Native authorizes both ends and forwards DTO. Extensions persist declarations/versioned edges/audit in one writer transaction; shared dependency assessment is consumed by lifecycle, retrieval and review. Native source refresh precedes permission, no guidance fail-open semantics changed.
**Tech:** Existing Go/SQLite/HTTP, Vue/TypeScript; no dependencies/root module change.

## Constraints / rulings

- Preserve007/008; use009 for supplement declarations. Preserve concurrent model-supervisor/front-end work.
- Same tenant/KB only;0 parents revokes,1–8 creates/replaces; no self/duplicate/cyclic relations. Default max dependency depth10/nodes64. Parents must be active/current/effective when declaring. Child may be pending/scheduled/active; removal may preserve a terminal child without reviving it.
- Ordinary version observation is not normative supersession. supplements links distinct document versions/families; original facts/edges/audit retained across revocations.
- A changed bound child or parent current native version invalidates the declaration; new declaration is explicit. Reads refresh current native roots; failure blocks retrieval, not permanent lifecycle mutation.
- A proven inactive/expired/version-changed dependency can move pending/scheduled/active child to stale. Review due only flags/notifies, never changes state. Date labels/annotations are not overwritten to simulate inheritance.
- Declaration+current revision+critical edge linkage+audit+idempotent receipt same transaction. Lifecycle transitions retain event/current/Outbox transaction. No new unsupported tag projection intent; relation tag UI belongs later governance projection.
- No source refresh network dependency in Host's initial local ready check. Background daily sweep can refresh first; core-offline failures reported, local date/state sweep still safe, retrieval remains fail-closed.

## Interfaces and files

1. New `extensions/dependencies/assessment.go` provides `Scope`, `ParentRef`, `Assessment` and `LoadTx(ctx,*sql.Tx,Scope) (Assessment,error)` for consistent current declaration/ancestry/lifecycle/version/date reading. `Assessment.Invalid(now)` and inherited `ReviewDue` are pure; no import of lifecycle/lineage to avoid cycles.
2. New009 migration: immutable declarations/normalized parents/current pointer/audit/receipts/edge linkages, composite scope FKs. Add supported version and strict prefix recognition; readonly review CLI explicitly known5–9, unknown10 rejected.
3. New `lineage.Supplements` with `Get(ctx,scope)`, `Put(ctx,SupplementCommand)`; command carries actor/op ID/CAS/parent IDs. Registry refreshes outside declaration transaction, then all heads/lifecycle/annotations/CAS rechecked within it. No public critical-edge generic writer.
4. `lifecycle/machine.go`, `automation.go` and new dependency tests: snapshot loads assessment; dedicated dependency-invalidated trigger guarded by latest transaction evidence. Existing terminal states never reactivate.
5. `retrieval/policy.go` uses real configured Registry to refresh bound sources before final dependency assessment. No registry on configured dependency means reject, never fallback. `review/` computes current advisory deadline from own/parent deadlines at scan/list/delivery and closes obsolete alerts; no state change.
6. `host/runtime.go` and new daily adapter instantiate fixed Core reader/Registry, management service and policy verifier. Keep local-ready ordering and task join.
7. Native `extension_lifecycle.go` authorizes child/all parents, derives actor; `extensionbridge/management.go` adds typed parents/op; `routes_knowledge.go` adds GET/PUT supplements. All business rules remain extensions.
8. `frontend/src/api/lifecycle.ts`, `views/lifecycle/{LifecycleView.vue,model.ts,model.test.ts}` add parent selector, revision-safe save and effective dependency reasons, retaining existing dates/tags.

## Test tasks

- [x] RED dependency assessment: unknown/new vs corrupted, parent inactive/date/review/version; transitive parents, cycle/depth/scope caps.
- [x] Implement schema009 and declaration service;16 CAS writers1winner, exact replay/conflicting actor/payload, preserved revocation facts; each SQL failure/child crash window rolls back all critical facts.
- [x] RED→GREEN lifecycle invalidation, policy immediate deny, parent review advisory and latest deadline checks; no revival/annotation rewrite.
- [x] Host auth/readiness and native parent/child ownership/JWT-only tests; wrong actor/scope/key/role, malformed/oversize body and expected revision.
- [x] Frontend behavior/type/build, dependency selector and CAS conflict tests.
- [x] Real isolated two-document upload→Observer/version capture→API declaration→parent expiry/version change→child RAG/Agent exclusion; keep existing active paths and all model-input canaries.
- [x] Complete extension build/test/vet/race/module/Python, affected native packages, frontend full checks and candidate build; one final review/fix pass; docs/backlog/evidence updates.

## Ledger

- T1/T2 complete evidence: `docs/evidence/m3-trusted-sources-2026-11-01.md`; last native8-package combination exit0, extension19packages/race0, frontend2368pass/1skip/type/build0; both real owned source+governance E2E0. No deployment.
- T3 implementation and API-level E2E complete; see `docs/evidence/m2-supplements-2026-11-01.md`. M2 remains incomplete pending T4/T5; no production deployment/migration or full native-app browser acceptance.
- Ruling: schema005 alerts FK to child annotations prevented inherited alerts for an unannotated child. Unpublished009 migrates existing alerts onto scoped review subjects, retains all rows and does not invent annotations. Published005/007/008 unchanged; real5–9 readonly/migration tests pass.
- Final independent review: shared-node depth bypass and concurrent parent closure growth fixed in one pass, both observed RED then GREEN; full suite/race/E2E rerun. Memoized subtree heights preserve limits on deeper revisits; final parent sources must be covered by this declaration's refreshed versions.
- Verification: extension build/vet/isolated serial fulltest0 (20 tested packages/3 no tests), isolated fullrace0, native8packages0 plus new authority cases0, frontend2369pass/1skip/type/build0, module/Python0 (31pass/2skip), candidateLite0, real supplement and existing governance E2E0. Ordinary parallel test/race had unrelated modelsupervisor closed-port probe failures, left unchanged; isolated serial rerun includes that package and passes.
- Frontend behavior/model/type/build and actual component browser clicks checked (synthetic HTTP API, real unmodified component); full native-app/user-database click-through unexecuted. Global diff-check has pre-existing/concurrent frontend whitespace; no unrelated cleanup.
