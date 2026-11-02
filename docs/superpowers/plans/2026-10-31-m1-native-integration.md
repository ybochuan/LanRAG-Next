# M1 Native Integration Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans and test-driven-development inline; one fresh whole-slice review at the end, one Important/Critical fix pass maximum.

**Goal:** Native PDF upload -> authenticated identity headers -> Host PDF preflight -> MinerU V1 -> document-bound middle.json -> unchanged native Markdown/chunk/index consumer.

**Spec:** User's approved three-file draft and 2026-10-31 instruction to implement it, permit explicitly configured proxy host IP, mount Host receiver, test/build and provide/run upload E2E.

## Constraints

- Preserve existing M1 work; HEAD fa6353e8c821daa5eeb667dc46140e4d0d32f179. No commits/pushes, root dependency/Frozen changes or native parsing/chunking refactors.
- Native production changes only knowledge_process.go, mineru_converter.go, new parser_proxy.go. All filtering/preflight/doc scope/archival live in extensions. Add meaningful tests.
- Native destination is explicitly configured by environment; allow host IP as user requested, pin each request to that origin, disable environment proxy/redirects. Host listener remains 127.0.0.1:8090.
- PDF files only; preserve Office/URL/audio paths. Reuse existing V1 ZIP/Markdown/images/layout conversion. Preserve native cancellation/timeouts; do not copy the retrieval gate's 500ms.
- Separate shared core-to-Host credential from Host-to-MinerU credential. Reject unsupported version, duplicate/missing/mismatched identity, unauthorized scope and absent binding; no configured-proxy failure fallback.
- Per-document child scopes are keyed by tenant/KB/doc to distinguish identical byte sources. Bounded in-memory state; no claim of durable recovery/SQL parser artifacts or complete M1 calibration.

## Tasks

- [x] 1. RED→GREEN native thin client, selected at convert entry before remote-reader availability; all HTTP legs including detached cancellation retain immutable identity. WSL CGO=1 tests and build, no dependency upgrades.
- [x] 2. RED→GREEN authenticated /parser/native V1 receiver, per-doc scope isolation, auto-bind before complete ACK, relative upload URLs, original artifact persistence before ZIP success.
- [x] 3. RED→GREEN Host parser config, fixed loopback routing/lifecycle, default off and existing M0 probe/worker behavior preserved. Module build/test/vet/boundary and Linux parser race.
- [x] 4. Automated isolated native upload E2E using rebuilt core, actual Host and installed MinerU4.0.1; temporary DB/files, authenticated public upload, deterministic embedding fixture, real parse/job/artifact/chunk checks; no model download/user-data mutation. Document CGO build and WSL/Windows address setup.
- [x] 5. One independent final review, one tested fix pass if needed, full applicable checks, update PROJECT_CONTEXT/TASK_BACKLOG/README/Source Mapping and evidence.

## Ledger

- Start: source unchanged since draft, previous M1 work retained. Ubuntu NAT and Windows MinerU are separate loopback namespaces. Ruling: do not alter system networking or broaden Host listener; E2E may use an owned test-only loopback relay if needed, and record exactly which topology was exercised.
- Tasks1–3: native and Host tests RED missing API → GREEN; identity/duplicate header/version/scope rejection, parallel client identity, detached cancel, same bytes two documents, native nonPDF/preflight and archive log boundaries covered. Original three production files only; no native result/chunk/scheduler refactor.
- Task4: default actual native upload exit0; full sentence comparison accounts only for Markdown underscore escaping. User later explicitly authorized live local models/DeepSeek generation and restarting8081/8082 with capability flags; real mode exit0 (explicit knowledge-search rerank + separately cited DeepSeek QA). Test-only relays, fresh DB, no global network edits, original binary/data retained. All earlier failed runs remain in evidence.
- Final review: one fresh read-only Astra review, Critical0/Important1/Minor0. Fixed slow bulk deadlines in one pass: real5.2s upload HTTP400→204, independent upstream bulk/metadata/cancellation regression RED→GREEN; original M0 timeouts unchanged. Extensions full13-package suite0, build/vet0, Linux parser/host/bridge race0; root CGO build0 and latest docparser full0. No rereview; live-model script additions subsequently verified by root.
- Validation exception: root service full package exit1 twice;49 failing leaf cases in structured rerun (48 include restricted/fake-IP outbound errors, one sandbox expected-call assertion). Current network resolves example.com/api.e2b.app to198.18.0.0/15. No SSRF changes/test weakening or claim of root-suite acceptance. Full root/frontend/production quality not run.
- Final Rulings and all review-declined scopes: see TASK_BACKLOG native-integration execution record (test topology, SQL/restart/capacity, OCR/Unicode/calibration, trusted-core ownership, existing-doc conflict refusal, quality acceptance, no full M0/preflight re-audit). Deferred minors:none. User forbids unrequested commit/push; working tree and evidence retained for review, not merged/released. Full report: docs/evidence/m1-native-integration-2026-10-31.md.
