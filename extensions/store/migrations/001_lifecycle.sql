CREATE TABLE ext_lifecycle_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL CHECK(length(to_state) > 0),
    reason TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    source_event_id TEXT,
    stage TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK(attempt >= 0),
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
    UNIQUE(tenant_id, knowledge_base_id, document_id, event_id),
    UNIQUE(tenant_id, knowledge_base_id, document_id, source_event_id, stage, attempt)
);

CREATE TABLE ext_lifecycle_current (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK(length(state) > 0),
    last_event_id TEXT NOT NULL REFERENCES ext_lifecycle_events(event_id),
    PRIMARY KEY(tenant_id, knowledge_base_id, document_id),
    FOREIGN KEY(tenant_id, knowledge_base_id, document_id, last_event_id)
        REFERENCES ext_lifecycle_events(tenant_id, knowledge_base_id, document_id, event_id)
);

CREATE TABLE ext_projection_outbox (
    outbox_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    projection_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    causation_type TEXT NOT NULL,
    causation_id TEXT NOT NULL,
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
    status TEXT NOT NULL CHECK(status IN ('pending','processing','done','dead')),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK(retry_count >= 0),
    next_retry_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    processed_at INTEGER,
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until INTEGER,
    UNIQUE(projection_type, aggregate_type, aggregate_id, causation_id),
    CHECK((status = 'processing' AND length(lease_token) > 0 AND lease_until IS NOT NULL)
        OR (status != 'processing' AND lease_token = '' AND lease_until IS NULL))
);

CREATE INDEX ext_outbox_pending ON ext_projection_outbox(status, next_retry_at, created_at);
CREATE INDEX ext_outbox_leases ON ext_projection_outbox(status, lease_until);
CREATE INDEX ext_outbox_scope ON ext_projection_outbox(tenant_id, knowledge_base_id, status);
