CREATE TABLE ext_family_suggestion_policies (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision > 0),
    similarity_weight REAL NOT NULL CHECK(similarity_weight BETWEEN 0 AND 1),
    declaration_weight REAL NOT NULL CHECK(declaration_weight BETWEEN 0 AND 1),
    structure_weight REAL NOT NULL CHECK(structure_weight BETWEEN 0 AND 1),
    threshold REAL NOT NULL CHECK(threshold > 0 AND threshold <= 1),
    CHECK(abs(similarity_weight + declaration_weight + structure_weight - 1.0) < 0.000001),
    PRIMARY KEY(tenant_id,knowledge_base_id)
);

CREATE TABLE ext_family_suggestions (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    suggestion_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('membership','relationship')),
    document_id TEXT NOT NULL,
    target_document_id TEXT NOT NULL,
    edge_type TEXT NOT NULL CHECK(edge_type IN ('','related_to','amends','interprets','supersedes')),
    source_version_id TEXT NOT NULL,
    target_version_id TEXT NOT NULL,
    evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
    confidence REAL NOT NULL CHECK(confidence BETWEEN 0 AND 1),
    policy_revision INTEGER NOT NULL CHECK(policy_revision >= 0),
    status TEXT NOT NULL CHECK(status IN ('pending','ignored','confirmed')),
    revision INTEGER NOT NULL CHECK(revision > 0),
    applied_id TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,suggestion_id),
    CHECK(document_id <> target_document_id),
    CHECK((kind='membership')=(edge_type='')),
    CHECK((status='confirmed')=(applied_id IS NOT NULL)),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id)
        REFERENCES ext_business_document_families(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id)
);

CREATE INDEX ext_family_suggestion_inbox
    ON ext_family_suggestions(tenant_id,knowledge_base_id,business_family_id,status,confidence DESC,suggestion_id);

CREATE TABLE ext_family_suggestion_scans (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    business_family_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision > 0),
    requested_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued','completed','failed')),
    PRIMARY KEY(tenant_id,knowledge_base_id,business_family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,business_family_id)
        REFERENCES ext_business_document_families(tenant_id,knowledge_base_id,business_family_id)
);
