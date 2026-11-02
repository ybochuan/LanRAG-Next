CREATE TABLE ext_review_alerts (
    alert_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    review_due TEXT NOT NULL,
    annotation_revision INTEGER NOT NULL CHECK(annotation_revision > 0),
    generation INTEGER NOT NULL CHECK(generation > 0),
    status TEXT NOT NULL CHECK(status IN ('open','resolved')),
    detected_at TEXT NOT NULL,
    delivered_at TEXT,
    resolved_at TEXT,
    UNIQUE(tenant_id,knowledge_base_id,document_id,review_due),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id)
        REFERENCES ext_document_annotations(tenant_id,knowledge_base_id,document_id),
    CHECK((status='open' AND resolved_at IS NULL) OR (status='resolved' AND resolved_at IS NOT NULL))
);
CREATE UNIQUE INDEX ext_review_one_open ON ext_review_alerts(tenant_id,knowledge_base_id,document_id) WHERE status='open';
CREATE INDEX ext_review_inbox ON ext_review_alerts(tenant_id,knowledge_base_id,status,detected_at,alert_id);
