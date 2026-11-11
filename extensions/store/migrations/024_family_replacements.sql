CREATE UNIQUE INDEX ext_business_family_member_fk
    ON ext_business_document_family_members(tenant_id,knowledge_base_id,business_family_id,document_id);

CREATE TABLE ext_family_replacements (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    replacement_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    family_revision INTEGER NOT NULL CHECK(family_revision > 0),
    old_document_id TEXT NOT NULL,
    old_version_node_id TEXT NOT NULL,
    new_document_id TEXT NOT NULL,
    new_version_node_id TEXT NOT NULL,
    effective_at_ms INTEGER NOT NULL CHECK(effective_at_ms > 0),
    status TEXT NOT NULL CHECK(status IN ('pending_review','scheduled','needs_review','revoked','applied')),
    revision INTEGER NOT NULL CHECK(revision > 0),
    applied_event_id TEXT UNIQUE,
    applied_at TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,replacement_id),
    CHECK(old_document_id <> new_document_id),
    CHECK((status='applied' AND applied_event_id IS NOT NULL AND applied_at IS NOT NULL)
       OR (status<>'applied' AND applied_event_id IS NULL AND applied_at IS NULL)),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id)
        REFERENCES ext_business_document_families(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,old_document_id,old_version_node_id)
        REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,new_document_id,new_version_node_id)
        REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(applied_event_id) REFERENCES ext_lifecycle_events(event_id)
);

CREATE UNIQUE INDEX ext_family_replacement_reservation
    ON ext_family_replacements(tenant_id,knowledge_base_id,business_family_id)
    WHERE status='scheduled';
CREATE INDEX ext_family_replacement_due
    ON ext_family_replacements(tenant_id,knowledge_base_id,effective_at_ms,replacement_id)
    WHERE status='scheduled';
CREATE INDEX ext_family_replacement_target_hold
    ON ext_family_replacements(tenant_id,knowledge_base_id,new_document_id)
    WHERE status='scheduled';

CREATE TABLE ext_business_document_family_heads (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    version_node_id TEXT NOT NULL,
    replacement_id TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id,document_id)
        REFERENCES ext_business_document_family_members(tenant_id,knowledge_base_id,business_family_id,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,version_node_id)
        REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,replacement_id)
        REFERENCES ext_family_replacements(tenant_id,knowledge_base_id,replacement_id)
);
