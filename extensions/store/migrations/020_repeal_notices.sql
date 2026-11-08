CREATE TABLE ext_repeal_notices (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    notice_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    target_document_id TEXT NOT NULL,
    source_version_id TEXT NOT NULL,
    target_version_id TEXT NOT NULL,
    effective_at_ms INTEGER NOT NULL CHECK(effective_at_ms > 0),
    status TEXT NOT NULL CHECK(status IN ('pending_review','scheduled','applied','revoked','needs_review')),
    revision INTEGER NOT NULL CHECK(revision > 0),
    applied_event_id TEXT UNIQUE,
    applied_at TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,notice_id),
    CHECK(document_id <> target_document_id),
    CHECK((status = 'applied' AND applied_event_id IS NOT NULL AND applied_at IS NOT NULL)
       OR (status <> 'applied' AND applied_event_id IS NULL AND applied_at IS NULL)),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(applied_event_id) REFERENCES ext_lifecycle_events(event_id)
);

CREATE UNIQUE INDEX ext_repeal_scheduled_target
    ON ext_repeal_notices(tenant_id,knowledge_base_id,target_document_id)
    WHERE status='scheduled';

CREATE INDEX ext_repeal_schedule
    ON ext_repeal_notices(tenant_id,knowledge_base_id,effective_at_ms,notice_id)
    WHERE status='scheduled';

CREATE VIEW ext_repeal_edges AS
SELECT 'repeal:'||notice_id AS edge_id,tenant_id,knowledge_base_id,
    source_version_id AS from_node_id,target_version_id AS to_node_id,
    'repeals' AS edge_type,'governance_critical' AS edge_class,applied_at AS created_at
FROM ext_repeal_notices WHERE status='applied';
