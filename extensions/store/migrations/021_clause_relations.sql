CREATE TABLE ext_clause_relations (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    relation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    target_document_id TEXT NOT NULL,
    source_version_id TEXT NOT NULL,
    target_version_id TEXT NOT NULL,
    source_chunk_id TEXT NOT NULL,
    target_chunk_id TEXT NOT NULL,
    source_content_hash TEXT NOT NULL CHECK(length(source_content_hash)=64),
    target_content_hash TEXT NOT NULL CHECK(length(target_content_hash)=64),
    edge_type TEXT NOT NULL CHECK(edge_type IN ('exception_to','conflicts_with')),
    pair_key TEXT NOT NULL,
    applies_when TEXT NOT NULL CHECK(length(applies_when)>0),
    status TEXT NOT NULL CHECK(status IN ('pending_review','active','awaiting_resolution','resolved','revoked')),
    revision INTEGER NOT NULL CHECK(revision>0),
    resolution TEXT NOT NULL DEFAULT '' CHECK(resolution IN ('','prefer_source','prefer_target','contextual')),
    decision_note TEXT NOT NULL DEFAULT '',
    resolved_by TEXT NOT NULL DEFAULT '',
    resolved_at TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,relation_id),
    CHECK(document_id<>target_document_id),
    CHECK(status<>'active' OR edge_type='exception_to'),
    CHECK(status NOT IN ('awaiting_resolution','resolved') OR edge_type='conflicts_with'),
    CHECK(status<>'resolved' OR (resolution<>'' AND decision_note<>'' AND resolved_by<>'' AND resolved_at IS NOT NULL)),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id)
        REFERENCES ext_source_snapshot_evidence(tenant_id,knowledge_base_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id)
        REFERENCES ext_source_snapshot_evidence(tenant_id,knowledge_base_id,version_node_id)
);

CREATE UNIQUE INDEX ext_clause_current_pair
    ON ext_clause_relations(tenant_id,knowledge_base_id,edge_type,pair_key) WHERE status<>'revoked';

CREATE VIEW ext_clause_eligible_documents AS
SELECT c.tenant_id,c.knowledge_base_id,c.document_id
FROM ext_lifecycle_current c
JOIN ext_lifecycle_events e ON e.tenant_id=c.tenant_id AND e.knowledge_base_id=c.knowledge_base_id
    AND e.document_id=c.document_id AND e.event_id=c.last_event_id AND e.to_state=c.state
LEFT JOIN ext_document_annotations a ON a.tenant_id=c.tenant_id AND a.knowledge_base_id=c.knowledge_base_id
    AND a.document_id=c.document_id
WHERE c.state='active'
AND (a.effective_from IS NULL OR julianday(a.effective_from)<=julianday('now'))
AND (a.effective_until IS NULL OR julianday(a.effective_until)>=julianday('now'))
AND NOT EXISTS (SELECT 1 FROM ext_lifecycle_events n WHERE n.tenant_id=c.tenant_id
    AND n.knowledge_base_id=c.knowledge_base_id AND n.document_id=c.document_id AND n.sequence>e.sequence)
AND NOT EXISTS (SELECT 1 FROM ext_repeal_notices r WHERE r.tenant_id=c.tenant_id
    AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
    AND r.status='scheduled' AND r.effective_at_ms<=CAST(unixepoch('subsec')*1000 AS INTEGER));

CREATE VIEW ext_clause_current AS
SELECT r.tenant_id,r.knowledge_base_id,r.relation_id
FROM ext_clause_relations r
JOIN ext_document_version_family a ON a.tenant_id=r.tenant_id AND a.knowledge_base_id=r.knowledge_base_id
    AND a.document_id=r.document_id AND a.current_version_node_id=r.source_version_id
JOIN ext_document_version_family b ON b.tenant_id=r.tenant_id AND b.knowledge_base_id=r.knowledge_base_id
    AND b.document_id=r.target_document_id AND b.current_version_node_id=r.target_version_id
JOIN ext_clause_eligible_documents ac ON ac.tenant_id=a.tenant_id AND ac.knowledge_base_id=a.knowledge_base_id
    AND ac.document_id=a.document_id
JOIN ext_clause_eligible_documents bc ON bc.tenant_id=b.tenant_id AND bc.knowledge_base_id=b.knowledge_base_id
    AND bc.document_id=b.document_id;

CREATE VIEW ext_clause_edges AS
SELECT 'clause:'||r.relation_id AS edge_id,r.tenant_id,r.knowledge_base_id,
    r.source_version_id AS from_node_id,r.target_version_id AS to_node_id,r.edge_type,r.created_at,r.status
FROM ext_clause_relations r JOIN ext_clause_current USING(tenant_id,knowledge_base_id,relation_id)
WHERE r.status IN ('active','awaiting_resolution','resolved');
