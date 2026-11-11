CREATE TABLE ext_business_document_families (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    label TEXT NOT NULL CHECK(length(trim(label)) BETWEEN 1 AND 120),
    revision INTEGER NOT NULL CHECK(revision > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id, knowledge_base_id, business_family_id)
);

CREATE TABLE ext_business_document_family_members (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    PRIMARY KEY(tenant_id, knowledge_base_id, document_id),
    FOREIGN KEY(tenant_id, knowledge_base_id, business_family_id)
        REFERENCES ext_business_document_families(tenant_id, knowledge_base_id, business_family_id),
    FOREIGN KEY(tenant_id, knowledge_base_id, document_id)
        REFERENCES ext_document_version_family(tenant_id, knowledge_base_id, document_id)
);

CREATE INDEX ext_business_document_family_member_lookup
    ON ext_business_document_family_members(tenant_id, knowledge_base_id, business_family_id, document_id);
