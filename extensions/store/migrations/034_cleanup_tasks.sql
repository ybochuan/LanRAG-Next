-- 30-day cleanup queue for native documents that have been soft-deleted.
-- Tasks are extension-only projections; no native row or file is ever removed here.
CREATE TABLE ext_cleanup_tasks (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    title_snapshot TEXT NOT NULL DEFAULT '',
    native_deleted_at TEXT NOT NULL DEFAULT '',
    first_observed_at TEXT NOT NULL,
    cleanup_due_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending_cleanup','awaiting_confirmation','held','cleaned','failed','cancelled')),
    hold_until TEXT,
    hold_reason TEXT NOT NULL DEFAULT '',
    notice_detected INTEGER NOT NULL DEFAULT 0,
    notice_7d INTEGER NOT NULL DEFAULT 0,
    notice_1d INTEGER NOT NULL DEFAULT 0,
    notice_due INTEGER NOT NULL DEFAULT 0,
    last_notified_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 1,
    cleaned_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);
CREATE INDEX ext_cleanup_tasks_due
    ON ext_cleanup_tasks(tenant_id,knowledge_base_id,status,cleanup_due_at);

-- One notice per knowledge base and stage per day; a repeated sweep must not
-- create a second bell notification for the same batch.
CREATE TABLE ext_cleanup_notices (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    notice_key TEXT NOT NULL,
    stage TEXT NOT NULL,
    item_count INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    delivered_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tenant_id,knowledge_base_id,notice_key)
);
CREATE INDEX ext_cleanup_notices_created
    ON ext_cleanup_notices(tenant_id,knowledge_base_id,created_at);

CREATE TABLE ext_cleanup_audit (
    audit_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    detail_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX ext_cleanup_audit_document
    ON ext_cleanup_audit(tenant_id,knowledge_base_id,document_id,created_at);

CREATE TABLE ext_cleanup_runs (
    run_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    candidate_count INTEGER NOT NULL DEFAULT 0,
    cleaned_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    trigger_name TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT ''
);
