# M1-03 PDF Cheap Preflight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Text-layer PDFs always reach MinerU as flash/txt, with real file preflight and explicitly document-bound middle.json archival.

**Architecture:** Pure Go pdfcpu reads bounded PDF objects/content, never OCR or MinerU during preflight. The proxy retains measurements after source-byte verification (including cache hits), applies effective policy before job deduplication, and archives the original middle.json from the completed ZIP when a trusted caller has bound a native doc_id.

**Tech Stack:** Go 1.26 module extensions; pdfcpu v0.16.4; stdlib HTTP/ZIP/filesystem; installed MinerU 4.0.1.

**Spec:** User's 2026-10-31 M1-03 instruction supersedes PDF tier escalation in Frozen 7.2; Frozen file itself remains unchanged.

## Constraints and decisions

- Preserve existing uncommitted M1-01 work; no core/root module/Frozen edits, commits, pushes, model downloads or original-data changes.
- Ruling: Any confirmed text layer selects flash/txt; mixed pages require review. Only exclusively image-painted pages may select basic/ocr. Ambiguous/unsupported inputs fail preflight. Cost: mixed scan text is not automatically recovered; layout-only escalation is deferred by the user's model-download rule.
- Ruling: pdfcpu offers decoded PDF content, not full linguistic text extraction. Recognize actual nonempty text-show operations with a font, including hidden OCR text and Form XObjects; do not equate resources/comments with text. Unsupported content fails safely. Cost: no claim of Unicode/garbling accuracy or calibrated corpus coverage.
- Ruling: Native V1 has no doc_id (C-12). Provide trusted in-process binding only; artifact-enabled jobs require it. No invented ID/header or core modification. Cost: production native identity wiring remains necessary; explicit scope POC is not native upload E2E.
- Artifact root is explicitly configured (production path data/parser_artifacts); private tests use temporary roots. Save middle.json without rewriting backend data; reject traversal, ambiguity, missing/invalid/oversized JSON, ownership mismatch and conflicting overwrite.

## Execution

- [x] 1. RED→GREEN: real text/image/mixed/empty/invalid/compressed/Form PDF fixtures, bounded/cancellable preflight and PDF routing. Files: extensions/go.mod, go.sum, parser/preflight.go, preflight_test.go, router.go, router_test.go.
- [x] 2. RED→GREEN: source-byte preflight retained through cold/cache uploads; caller auto/ocr/advanced cannot bypass flash/txt. Files: parser/scope.go, mineru_upload.go, mineru_jobs.go, mineru_proxy.go and tests.
- [x] 3. RED→GREEN: explicit native document binding and bounded atomic middle.json extraction on completed ZIP download. Files: parser/artifacts.go, artifacts_test.go and proxy integration tests.
- [x] 4. Extend and run the real MinerU 4.0.1 POC with caller auto/ocr, wire-level flash/txt assertions, document-bound artifact checks, private offline model home and no ONNX; report under docs/evidence. Files: parser/mineru_proxy_test.go, scripts/m1_scope_poc.py and tests as needed.
- [x] 5. Run format/build/test/vet/module-boundary/Python checks, Linux parser race; preserve Outbox baseline failures honestly. One fresh final whole-slice review and at most one Important/Critical fix pass, no second review. Update README, PROJECT_CONTEXT, TASK_BACKLOG and Source Mapping with current facts/limits.

## Ledger

- Start: HEAD fa6353e8c821daa5eeb667dc46140e4d0d32f179, v0.9.0-2-gfa6353e; pre-existing M1-01 files preserved. Module query/download pinned pdfcpu v0.16.4 (Go 1.26). No production Host mounting or native auth changes authorized.

- Pre-flight interfaces: 1→2 measures verified bytes before source grant/job dispatch; 2→3 carries explicit document assertion to completed ZIP ownership; 3→4 checks original middle/schema with the real backend. Source bytes are not retained; bounded measurements are retained. No core IDs are invented.
- Task 1/2: RED→GREEN recorded in TASK_BACKLOG; pure-Go parser, real fixtures and conservative PDF policy; caller policy overridden before effective-request deduplication.
- Task 3: missing artifact API first failed compile; explicit binding, completed-output gate, original-byte middle+manifest directory publication and eight concurrent idempotent writes passed. No artifact SQL/power-loss/restart guarantee.
- Task 4: initial real POC 01:49:14 exit0; final after review fix 02:05:03 exit0, docs/evidence/m1-preflight-2026-10-31-final.md. Caller auto/ocr+advanced -> actual flash/txt, 1 page/1 text, two jobs for two scopes, cache hit, schema2.0 middle saved, no audited Python external DNS/TCP attempt or private ONNX. Native upload/authorization is outside the POC.
- Final review: fresh gpt-6-astra/high read-only whole slice, Critical0/Important1/Minor0. ActualText OCR bypass confirmed; direct/named/structure and proxy tests RED→GREEN, unknown-op ambiguity regression also RED→GREEN. One fix pass only; no second reviewer invocation.
- Final: fixed ActualText/unknown-content OCR permission -- named/direct marked properties decoded, unresolved structure/unknown ops cannot prove a scan; final full parser suite green, full 13-package suite exit0, Linux parser race exit0.
- Final: Ruling: reviewer declined same-scope cache file_id -> multiple doc_id design; retain safe refusal -- no false ownership or hidden core contract expansion -- cost: duplicate-content multiple-document case remains unsupported (M1-04D). Optional artifacts/in-process asserted identity/Host not mounted and full Unicode/calibration/M1 acceptance remain explicitly outside current evidence.
- Final: Ruling: network proof is only Python socket audits plus private directory weights and verified effective HTTP policy -- no OS-wide/native-code network-isolation claim -- cost: no assertion about unobserved library/system paths or all PDFs.
- Verification: final Go build/vet/format, 13-package tests, module boundary306 imports, Python21 (1 Windows skip), WSL parser race and real POC exit0. Initial full Go run exit1 from old Outbox calls=3/reports=2 retained; not fixed/waived for M1. Core/root manifests/Frozen/baseline unchanged; final git diff/scope checks0. Upstream/frontend/real OCR/native ingestion E2E/SQL crash recovery/calibration not run.
- Completed checklist items 1-5 for this PDF slice. M1 overall and production wiring remain incomplete. No commits/pushes.
