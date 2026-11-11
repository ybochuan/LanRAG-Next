CREATE TABLE ext_document_availability (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active','missing','deleted')),
    observed_at TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);
