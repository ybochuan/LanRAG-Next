CREATE TABLE ext_business_family_member_history (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active','removed')),
    removed_at TEXT,
    reason TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,business_family_id,document_id),
    CHECK((status='active' AND removed_at IS NULL AND reason IS NULL)
       OR (status='removed' AND removed_at IS NOT NULL AND reason IS NOT NULL)),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id)
        REFERENCES ext_business_document_families(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id)
        REFERENCES ext_document_version_family(tenant_id,knowledge_base_id,document_id)
);

INSERT INTO ext_business_family_member_history(tenant_id,knowledge_base_id,business_family_id,document_id,status)
SELECT tenant_id,knowledge_base_id,business_family_id,document_id,'active'
FROM ext_business_document_family_members;
