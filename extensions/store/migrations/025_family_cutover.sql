CREATE TABLE ext_business_family_legacy_mappings (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    legacy_family_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    head_document_id TEXT NOT NULL,
    head_version_node_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),
    operation_id TEXT NOT NULL,
    applied_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,legacy_family_id),
    UNIQUE(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,legacy_family_id)
        REFERENCES ext_document_version_family(tenant_id,knowledge_base_id,family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id)
        REFERENCES ext_business_document_families(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,head_document_id,head_version_node_id)
        REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,operation_id)
        REFERENCES ext_business_receipts(tenant_id,knowledge_base_id,operation_id) DEFERRABLE INITIALLY DEFERRED
);

CREATE VIEW ext_business_family_published_heads AS
SELECT tenant_id,knowledge_base_id,business_family_id,document_id,version_node_id
FROM ext_business_document_family_heads
UNION ALL
SELECT m.tenant_id,m.knowledge_base_id,m.business_family_id,m.head_document_id,m.head_version_node_id
FROM ext_business_family_legacy_mappings m
WHERE NOT EXISTS (
    SELECT 1 FROM ext_business_document_family_heads h WHERE h.tenant_id=m.tenant_id
    AND h.knowledge_base_id=m.knowledge_base_id AND h.business_family_id=m.business_family_id
);
