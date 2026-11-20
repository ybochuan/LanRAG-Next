ALTER TABLE ext_document_relations RENAME TO ext_document_relations_v37;

CREATE TABLE ext_document_relations (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    relation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    target_knowledge_base_id TEXT NOT NULL,
    target_document_id TEXT NOT NULL,
    source_version_id TEXT NOT NULL,
    target_version_id TEXT NOT NULL,
    edge_type TEXT NOT NULL CHECK(edge_type IN ('amends','interprets','related_to','cites','implements','template_for','explains_more','reading_note')),
    status TEXT NOT NULL CHECK(status IN ('candidate','active','revoked')),
    confidence REAL NOT NULL CHECK(confidence>=0 AND confidence<=1),
    revision INTEGER NOT NULL,
    edge_id TEXT,
    cross_edge_id TEXT,
    attachment_reference_json TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,relation_id),
    CHECK(document_id<>target_document_id),
    CHECK(edge_type NOT IN ('explains_more','reading_note') OR knowledge_base_id=target_knowledge_base_id),
    CHECK(edge_id IS NULL OR cross_edge_id IS NULL),
    CHECK(cross_edge_id IS NULL OR edge_type IN ('amends','interprets','related_to')),
    CHECK(edge_type NOT IN ('cites','implements','template_for','explains_more','reading_note') OR (edge_id IS NULL AND cross_edge_id IS NULL AND status<>'candidate')),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,target_knowledge_base_id,target_version_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(edge_id) REFERENCES ext_lineage_edge(edge_id),
    FOREIGN KEY(cross_edge_id) REFERENCES ext_lineage_edge_cross_kb(edge_id)
);

INSERT INTO ext_document_relations (
    tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
    source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id,attachment_reference_json
)
SELECT tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
    source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id,attachment_reference_json
FROM ext_document_relations_v37;
DROP TABLE ext_document_relations_v37;

CREATE INDEX ext_document_relations_outgoing
    ON ext_document_relations(tenant_id,knowledge_base_id,document_id,source_version_id,status);
CREATE INDEX ext_document_relations_incoming
    ON ext_document_relations(tenant_id,target_knowledge_base_id,target_document_id,target_version_id,status);
