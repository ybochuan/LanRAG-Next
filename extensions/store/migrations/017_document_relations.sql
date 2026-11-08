ALTER TABLE ext_document_relations RENAME TO ext_document_relations_v11;

CREATE TABLE ext_document_relations (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, relation_id TEXT NOT NULL,
 document_id TEXT NOT NULL, target_document_id TEXT NOT NULL,
 source_version_id TEXT NOT NULL, target_version_id TEXT NOT NULL,
 edge_type TEXT NOT NULL CHECK(edge_type IN ('amends','interprets','related_to','cites','implements','template_for')),
 status TEXT NOT NULL CHECK(status IN ('candidate','active','revoked')),
 confidence REAL NOT NULL CHECK(confidence>=0 AND confidence<=1), revision INTEGER NOT NULL,
 edge_id TEXT, PRIMARY KEY(tenant_id,knowledge_base_id,relation_id),
 CHECK(document_id<>target_document_id),
 CHECK(edge_type NOT IN ('cites','implements','template_for') OR (edge_id IS NULL AND status<>'candidate')),
 FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
 FOREIGN KEY(edge_id) REFERENCES ext_lineage_edge(edge_id)
);

INSERT INTO ext_document_relations SELECT * FROM ext_document_relations_v11;
DROP TABLE ext_document_relations_v11;
CREATE INDEX ext_document_relations_outgoing ON ext_document_relations(tenant_id,knowledge_base_id,document_id,source_version_id,status);
CREATE INDEX ext_document_relations_incoming ON ext_document_relations(tenant_id,knowledge_base_id,target_document_id,target_version_id,status);
