# M2 lifecycle core plan and ledger

Spec: docs/design/M2-Lifecycle-Core-2026-10-31.md; Frozen4.5–4.10/16 and user default-matrix clarification.

Inline execution and TDD; one final independent review. Preserve M1 work; no commits/pushes, native updates or dependency changes.

- [x] Outbox RED: canceled parent reported1 instead of0; GREEN preserves business failure and original strict drain test.
- [x] Lifecycle RED: missing Machine/Configuration/Annotations; implement approved default matrix and database config. Unconfigured means permanent.
- [x] Transaction guard; expected head/annotation/policy fences; immutable receipt; Initialize-only date defaults.
- [x] Separate Document/Guidance stale and explicit supplement evidence assessment.
- [x] Five-write SQL failure injection, two 24-writer tests, actual process exit during/after Apply, FK/scope/ABA and v1/v2/v3 upgrade.
- [x] Full extension checks, Linux race, one final independent review (no findings), evidence and status synchronization. See docs/evidence/m2-lifecycle-core-2026-10-31.md. No second review or fix pass required.

Ruling: user's matrix replaces provisional operator-defined graph — new business authority — editable graph is outside this slice.
Ruling: use database config — user allowed either DB or TOML — deployment API is not exposed yet.
Ruling: future-dated parsed pending enters scheduled — prevents premature activation — already-expired pending/scheduled needs a later operating rule.
Ruling: date defaults use initialization clock/effective_from as documented — user supplied days but not anchors — later business changes must explicitly migrate annotations.
Ruling: no Observer/background binding in core slice — no native changes or fabricated parse observations — automatic migration and alerts remain pending, M2 is not accepted as a whole.
Ruling: keep Store.Append as M0 low-level API — compatibility — not universal policy enforcement.
