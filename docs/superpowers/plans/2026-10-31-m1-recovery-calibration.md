# M1 recovery and calibration implementation

Scope: extension-only follow-up to the approved native PDF integration. Preserve all existing changes. No core algorithm changes, dependency upgrades or corpus fabrication.

1. Add migration 003 for durable parser document snapshots, formal job intents/receipts and original middle JSON artifacts. Keep artifact -> formal job foreign keys and upgrade existing extension DBs without touching native DBs.
2. Deduplicate by physically verified source hash, trusted tenant/KB/doc, upstream origin and effective parser policy. Commit intent before remote POST. A lost/invalid response remains unknown and must never trigger an automatic second parse POST.
3. Persist authenticated document capabilities and known receipts before success ACK. Restore after restart. Serialize requests per document; evict only idle, successfully persisted scopes so sequential workloads can exceed 128 documents. Concurrent active capacity remains bounded.
4. Publish filesystem artifacts first, then transactionally register the original JSON and metadata. Retry verifies existing identical files and repairs a missing SQL registration. Never return ZIP success if registration failed. Filesystem + SQLite are a recovery protocol, not one atomic resource transaction; physical power-loss guarantees need separate validation.
5. Implement offline, per-KB held-out calibration measurement with explicit expected labels and leakage rejection. No real corpus currently available (user confirmed); synthetic tests validate the measurement tool only and do not satisfy the 85/10/15 acceptance gate.
6. Run targeted RED/GREEN tests, full extension checks, Linux race checks, native upload integration and M0 joint regression where available. Record exact outcomes and remaining limitations, then one independent review of this slice.

Conflicts: MinerU V1 lacks a trusted caller idempotency key/unique document correlation for an unknown create-job outcome. Persist and refuse ambiguous retries; do not guess ownership from a global job listing. Native scheduler retries, full image/garble/layout detection and physical power testing are not silently claimed solved.
