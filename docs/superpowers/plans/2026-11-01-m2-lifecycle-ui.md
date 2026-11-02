# M2 Lifecycle UI Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans, inline execution and one final independent review. User approved native integration, thin management facade and direct implementation; no further design gate. No commits/pushes.

**Goal:** Deliver `/platform/lifecycle` in the existing Vue UI with scoped state overview, dates, event history, overdue reviews and KB defaults.
**Architecture:** Native JWT/KB grants authorize every request; a fixed-loopback HTTP adapter forwards server-derived tenant/KB/read-write authority. Extension management owns all DB reads/configuration and uses existing lifecycle services. Browser never receives the management credential.
**Stack:** Existing Vue/TDesign/TypeScript, Gin, independent Go/net/http/SQLite extension. No new dependencies.
**Spec:** User's 2026-11-01 integration approval; Frozen Architecture §13; existing M2 Configuration/automation/review contracts.

## Constraints and decisions

- Preserve dirty M1/M2/governance work; root module dependencies untouched; do not refactor native retrieval/Agent/ingestion. Reuse `KnowledgeBaseHandler` native access resolution.
- JWT-only management endpoints; KB read permission for GET, native ownership/write guards plus explicit Editor grant for PUT. Handler checks remain active even when native RBAC is disabled.
- Host business scopes remain configured tenant/KB only. Dedicated `AGENTRAG_MANAGEMENT_KEY`, server-side Bearer, version header 1. Missing key disables management, never unauthenticated fallback. Existing before_retrieve stays 500ms; management has separate 2s timeout.
- Dates use UTC transport/local display; CAS conflicts return409, client reloads rather than retries. Full annotation snapshot preserves labels. Dates save first, then existing Timer evaluation; uncertain evaluation is reported pending, not fake success. Never revive stale or expose direct state writes.
- KB default days are integers 0..36500; zero disables that default, only new initialization affected. KB row replaces inherited defaults; editing uses local revision, not inherited revision.
- Frontend Host errors remain visible and do not appear as zero/active. Rapid scope changes must discard outdated responses.
- Existing Windows Core18880 / WSL Bridge8090 unchanged. Management readiness distinct from business hook health.

## Shared wire contract

Core paths under `/api/v1/knowledge-bases/:id/lifecycle`:
- GET `?page=1&page_size=20&state=`: overview `{counts,total,page,page_size,documents,rules,rule_revision,can_write}`.
- GET `/documents/:document_id`: `{snapshot,events,evaluation_status?}`. Snapshot is existing lifecycle Snapshot.
- PUT `/documents/:document_id`: `{expected_revision,annotations}`; same document response, CAS409. Server derives scope, no client tenant/KB.
- GET `/rules`: `{rules,rule_revision}` (rules is existing ResolvedRules).
- PUT `/rules`: `{expected_revision,rules:{effective_delay_days,valid_days,review_days}}`.
- GET `/reviews`: `{alerts,truncated}` using existing scoped review inbox, max1000 with explicit truncation.

All success wrappers `{success:true,data:...}`, safe errors `{success:false,error:{code,message}}`. Internal POST `/management/lifecycle` body `{action,tenant_id,knowledge_base_id,document_id,page,page_size,state,can_write,expected_revision,annotations,rules}`. Only facade constructs authority. Action whitelist overview/document/put_annotations/rules/put_rules/reviews. Document rows `{document_id,state,last_event_id,annotations}`; native metadata fetched independently through existing authorized document API, missing titles display ID.

## Tasks and verification

### 1. Scoped extension management
Files `extensions/management/{server,read}.go` and tests; `host/{config,runtime}.go`; `lifecycle/automation.go` (export existing single-doc Timer evaluation); `scripts/launcher_config.py`.
- [x] RED tests: missing/wrong/version credentials, scope spoofing, readonly writes, stale revision, metadata preservation, dates→stale and no revival.
- [x] Implement bounded query/read DTOs and existing Configuration/Machine calls; test concurrent writes one winner.
- [x] Host route optional/authenticated, dedicated secret auto-created in private launcher configuration. Run extension tests/build/vet/race and module boundary.

### 2. Native facade
Files `internal/extensionbridge/management.go`, `internal/handler/extension_lifecycle.go`, `internal/router/routes_knowledge.go`, tests.
- [x] RED tests: no JWT, API key denied, KB read vs write/tenant mismatch, HTTP503/malformed/timeout, fixed URL/path restrictions.
- [x] Reuse native KB grant, strict body input, forward only approved DTO/action/derived identity. Safe error/limited response.
- [x] Scoped tests +CGO Lite build; root dependency hashes unchanged.

### 3. UI
Files `frontend/src/views/lifecycle/{LifecycleView.vue,model.ts,model.test.ts}`, `frontend/src/api/lifecycle.ts`, router/menu minimal additions.
- [x] RED domain tests for dates/timezone/CAS payload preservation/rule bounds/request sequencing.
- [x] Native-style status cards, KB selector, paged/filterable list, detail/date drawer, event timeline, review tab and rules editor; readonly/error/loading/empty handling.
- [x] Type-check, relevant tests, full frontend suite (record baseline failures separately), production build; no extra dependency install if existing modules work.

### 4. Delivery
- [x] Real Core/Host/API E2E with private DB: login, document active, edit expiry, stale exclusion, rules CAS, readonly/cross-scope rejection. Browser UI verify layout and actions, preserving user documents.
- [x] One fresh security/quality review; fix important findings once and verify.
- [x] Install built web/Core/Host with private backup, preserve18880 and secrets. Update docs/backlog accurately; diff/check/status/include untracked scope.

## Ledger

- Preflight: HEAD fa6353e, baseline v0.9.0; dirty work preserved. Frontend `npm run type-check` baseline exit0.
- Ruling: Source interface approval is sufficient for direct implementation; do not reopen design approval. Interface names/DTO above are frozen for this task; implementation may narrow unsafe capability, with reasons recorded here.

- Delivered 2026-11-01. Independent review fixed split rule snapshots and unsupported effective-date edits. Real isolated E2E passed; production browser read-only checks passed. Detailed commands and remaining baseline failures: `docs/evidence/m2-lifecycle-ui-validation-2026-11-01.md`.
