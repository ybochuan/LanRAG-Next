# M1-03 PDF preflight / MinerU4.0.1

```text
[RUN] 2026-10-31 01:49:14 Asia/Shanghai
[SCOPE] Real MinerU4.0.1; PDF preflight/scoped V1/artifact POC; native upload API and production Host are not exercised
[VERSION] Existing interpreter verified: MinerU4.0.1; no install or upgrade
[BACKEND] Owned random IPv4 loopback port; Flash only, model preloading disabled, private home/uploads
[MODE] Caller auto/ocr+advanced; real PDF preflight must force flash/txt; external DNS/TCP is guarded
=== RUN   TestScopeProxyRealMinerU401
    mineru_proxy_test.go:516: preflight pages=1 text=1; caller auto/ocr+advanced reached backend as flash/txt; middle.json saved under explicit fixture doc_id, schema=2.0
    mineru_proxy_test.go:516: preflight pages=1 text=1; caller auto/ocr+advanced reached backend as flash/txt; middle.json saved under explicit fixture doc_id, schema=2.0
    mineru_proxy_test.go:527: real backend cache hit verified; two byte-proven scopes, two total formal jobs
--- PASS: TestScopeProxyRealMinerU401 (8.57s)
PASS
[PROTOCOL] PASS: cold/hot byte proof, PDF measurements, forced flash/txt, cross-scope refusal, one job per scope and document-bound middle.json
[CLEANUP] Owned processes stopped
[RESOURCES] PASS: zero external DNS/TCP attempts and zero ONNX weights in private work directory
[STATE] Native DB, venv and user model configuration unchanged
[LIMIT] Synthetic doc_id bindings only; no native upload/KB authorization, automatic native doc binding, artifact DB transaction, restart recovery, corpus calibration, actual OCR or full M1 acceptance
[RESULT] PASS
```
