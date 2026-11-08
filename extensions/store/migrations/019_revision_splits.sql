CREATE TABLE ext_revision_split_batches (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    source_document_id TEXT NOT NULL,
    source_version_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending_review','approved','rejected')),
    revision INTEGER NOT NULL CHECK(revision > 0),
    first_proposal_id TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,batch_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,first_proposal_id)
        REFERENCES ext_revision_proposals(tenant_id,knowledge_base_id,proposal_id)
);
CREATE UNIQUE INDEX ext_revision_split_approved_source
    ON ext_revision_split_batches(tenant_id,knowledge_base_id,source_document_id,source_version_id)
    WHERE status='approved';

CREATE TABLE ext_revision_split_parts (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    proposal_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 8),
    title TEXT NOT NULL CHECK(length(title) > 0),
    target_version_id TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,proposal_id),
    UNIQUE(tenant_id,knowledge_base_id,batch_id,position),
    FOREIGN KEY(tenant_id,knowledge_base_id,batch_id)
        REFERENCES ext_revision_split_batches(tenant_id,knowledge_base_id,batch_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,proposal_id)
        REFERENCES ext_revision_proposals(tenant_id,knowledge_base_id,proposal_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id)
);
CREATE INDEX ext_revision_target_lookup
    ON ext_revision_proposals(tenant_id,knowledge_base_id,target_document_id);

CREATE VIEW ext_revision_split_ancestry AS
SELECT 'split:'||p.proposal_id AS edge_id,p.tenant_id,p.knowledge_base_id,
    p.target_version_id AS from_node_id,b.source_version_id AS to_node_id,
    'split_from' AS edge_type,'governance_critical' AS edge_class,r.published_at AS created_at
FROM ext_revision_split_parts p
JOIN ext_revision_split_batches b USING(tenant_id,knowledge_base_id,batch_id)
JOIN ext_revision_proposals r USING(tenant_id,knowledge_base_id,proposal_id)
WHERE b.status='approved' AND b.first_proposal_id IS NOT NULL
    AND p.target_version_id IS NOT NULL AND r.status IN ('published','superseded');
