CREATE TABLE ext_business_receipts (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, operation_id TEXT NOT NULL,
 payload_hash TEXT NOT NULL, result_json TEXT NOT NULL CHECK(json_valid(result_json)),
 PRIMARY KEY(tenant_id,knowledge_base_id,operation_id)
);
CREATE TABLE ext_business_audit (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
 operation_id TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, aggregate_id TEXT NOT NULL,
 revision INTEGER NOT NULL, status TEXT NOT NULL, occurred_at TEXT NOT NULL, payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 FOREIGN KEY(tenant_id,knowledge_base_id,operation_id) REFERENCES ext_business_receipts(tenant_id,knowledge_base_id,operation_id)
);
CREATE TABLE ext_document_relations (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, relation_id TEXT NOT NULL,
 document_id TEXT NOT NULL, target_document_id TEXT NOT NULL,
 source_version_id TEXT NOT NULL, target_version_id TEXT NOT NULL, edge_type TEXT NOT NULL CHECK(edge_type IN ('amends','interprets','related_to')),
 status TEXT NOT NULL CHECK(status IN ('candidate','active','revoked')), confidence REAL NOT NULL CHECK(confidence>=0 AND confidence<=1), revision INTEGER NOT NULL,
 edge_id TEXT, PRIMARY KEY(tenant_id,knowledge_base_id,relation_id),
 CHECK(document_id<>target_document_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,target_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
 FOREIGN KEY(edge_id) REFERENCES ext_lineage_edge(edge_id)
);
CREATE TABLE ext_feedback_events (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, event_id TEXT NOT NULL,
 source TEXT NOT NULL CHECK(source IN ('manual','seed')), topic TEXT NOT NULL,
 text TEXT NOT NULL, negative INTEGER NOT NULL CHECK(negative IN (0,1)), occurred_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,knowledge_base_id,event_id)
);
CREATE TABLE ext_feedback_staging (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL, topic TEXT NOT NULL,
 text TEXT NOT NULL, risk TEXT NOT NULL CHECK(risk IN ('low','medium','high')),
 status TEXT NOT NULL CHECK(status IN ('pending_review','approved','rejected','quarantined','active','withdrawn')),
 revision INTEGER NOT NULL, ready_at TEXT, PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id) REFERENCES ext_feedback_events(tenant_id,knowledge_base_id,event_id)
);
CREATE TABLE ext_feedback_guidances (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, guidance_id TEXT NOT NULL,
 feedback_id TEXT, source TEXT NOT NULL CHECK(source IN ('feedback','seed')), topic TEXT NOT NULL,
 text TEXT NOT NULL, confidence REAL NOT NULL CHECK(confidence>=0 AND confidence<=1),
 status TEXT NOT NULL CHECK(status IN ('active','quarantined','withdrawn')), revision INTEGER NOT NULL,
 created_at TEXT NOT NULL, PRIMARY KEY(tenant_id,knowledge_base_id,guidance_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id) REFERENCES ext_feedback_staging(tenant_id,knowledge_base_id,feedback_id)
);
CREATE TABLE ext_revision_proposals (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, proposal_id TEXT NOT NULL,
 source_document_id TEXT NOT NULL, source_version_id TEXT NOT NULL, family_id TEXT NOT NULL, trigger_guidance_ids TEXT NOT NULL CHECK(json_valid(trigger_guidance_ids)),
 proposed_patch TEXT NOT NULL, target_content_hash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending_review','approved','publishing','publish_failed','indexed','published','superseded','rejected')),
 revision INTEGER NOT NULL, publication_key TEXT NOT NULL, target_document_id TEXT NOT NULL DEFAULT '',
 publish_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, published_at TEXT, publish_lease_until TEXT,
 PRIMARY KEY(tenant_id,knowledge_base_id,proposal_id), UNIQUE(tenant_id,knowledge_base_id,publication_key),
 FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,family_id) REFERENCES ext_document_version_family(tenant_id,knowledge_base_id,family_id)
);
