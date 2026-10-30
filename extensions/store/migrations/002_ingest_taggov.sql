CREATE TABLE ext_ingest_observations (
    observation_id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    parse_status TEXT NOT NULL,
    source_updated_at TEXT NOT NULL,
    observed_at INTEGER NOT NULL,
    origin TEXT NOT NULL CHECK(origin='reconcile'),
    UNIQUE(tenant_id,knowledge_base_id,document_id,source_updated_at),
    UNIQUE(tenant_id,knowledge_base_id,document_id,observation_id)
);

CREATE TABLE ext_ingest_current (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    last_observation_id INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,last_observation_id)
        REFERENCES ext_ingest_observations(tenant_id,knowledge_base_id,document_id,observation_id)
);

CREATE TABLE ext_kb_tag_mapping (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    tag_id TEXT NOT NULL,
    verified_at INTEGER NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,canonical_name)
);

CREATE TABLE ext_tag_sync_failures (
    failure_id INTEGER PRIMARY KEY AUTOINCREMENT,
    outbox_id TEXT NOT NULL REFERENCES ext_projection_outbox(outbox_id),
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    occurred_at INTEGER NOT NULL
);
CREATE INDEX ext_tag_failures_scope ON ext_tag_sync_failures(tenant_id,knowledge_base_id,document_id);
