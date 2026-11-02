CREATE TABLE ext_lifecycle_rules (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision > 0),
    rules_json TEXT NOT NULL CHECK(json_valid(rules_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id)
);

CREATE TABLE ext_document_annotations (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision > 0),
    effective_from TEXT,
    effective_until TEXT,
    review_due TEXT,
    labels_json TEXT NOT NULL CHECK(json_valid(labels_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);

CREATE TABLE ext_lifecycle_decisions (
    event_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    command_json TEXT NOT NULL CHECK(json_valid(command_json)),
    annotations_json TEXT NOT NULL CHECK(json_valid(annotations_json)),
    policy_json TEXT NOT NULL CHECK(json_valid(policy_json)),
    evaluated_at TEXT NOT NULL,
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,event_id)
        REFERENCES ext_lifecycle_events(tenant_id,knowledge_base_id,document_id,event_id)
        DEFERRABLE INITIALLY DEFERRED
);
