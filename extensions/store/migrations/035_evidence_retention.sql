-- Retention sweeps record only aggregate counts. Evidence rows are deleted
-- in bounded batches; no original run, lifecycle or governance fact is rewritten.
CREATE TABLE ext_evidence_retention_runs (
    run_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    cutoff_at TEXT NOT NULL,
    trigger_name TEXT NOT NULL DEFAULT '',
    runs_scanned INTEGER NOT NULL DEFAULT 0,
    receipts_deleted INTEGER NOT NULL DEFAULT 0,
    audit_deleted INTEGER NOT NULL DEFAULT 0,
    inputs_deleted INTEGER NOT NULL DEFAULT 0,
    intents_deleted INTEGER NOT NULL DEFAULT 0,
    steps_deleted INTEGER NOT NULL DEFAULT 0,
    evidence_deleted INTEGER NOT NULL DEFAULT 0,
    tag_failures_deleted INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ext_evidence_retention_recent
    ON ext_evidence_retention_runs(tenant_id,knowledge_base_id,started_at);

CREATE INDEX ext_provenance_audit_retention
    ON ext_provenance_run_audit(tenant_id,knowledge_base_id,occurred_at);
CREATE INDEX ext_tag_failures_retention
    ON ext_tag_sync_failures(tenant_id,knowledge_base_id,occurred_at);
CREATE INDEX ext_ingest_observations_retention
    ON ext_ingest_observations(tenant_id,knowledge_base_id,observed_at);
CREATE INDEX ext_document_version_retention
    ON ext_document_version(tenant_id,knowledge_base_id,created_at);
