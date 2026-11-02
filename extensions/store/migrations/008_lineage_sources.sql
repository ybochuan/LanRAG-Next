CREATE TABLE ext_lineage_node (
    node_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    node_type TEXT NOT NULL CHECK(node_type IN ('document_version','chunk','parser_artifact','chunk_revision')),
    native_id TEXT NOT NULL, created_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,node_id)
);
CREATE TABLE ext_document_version_family (
    family_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    family_label TEXT NOT NULL DEFAULT '',
    root_version_node_id TEXT NOT NULL, current_version_node_id TEXT NOT NULL,
    observation_sequence INTEGER NOT NULL CHECK(observation_sequence>0),
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,family_id),
    UNIQUE(tenant_id,knowledge_base_id,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,root_version_node_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,current_version_node_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id)
);
CREATE TABLE ext_document_version (
    version_node_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, family_id TEXT NOT NULL,
    document_id TEXT NOT NULL, content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    observation_sequence INTEGER NOT NULL CHECK(observation_sequence>0),
    lifecycle_event_id TEXT NOT NULL, source_uri TEXT NOT NULL DEFAULT '',
    source_format TEXT NOT NULL, created_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,version_node_id),
    UNIQUE(tenant_id,knowledge_base_id,document_id,observation_sequence),
    FOREIGN KEY(tenant_id,knowledge_base_id,version_node_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,family_id) REFERENCES ext_document_version_family(tenant_id,knowledge_base_id,family_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,lifecycle_event_id) REFERENCES ext_lifecycle_events(tenant_id,knowledge_base_id,document_id,event_id)
);
CREATE TABLE ext_source_snapshot_evidence (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, version_node_id TEXT NOT NULL,
    snapshot_json TEXT NOT NULL CHECK(json_valid(snapshot_json)),
    evidence_hash TEXT NOT NULL CHECK(length(evidence_hash)=64),
    PRIMARY KEY(tenant_id,knowledge_base_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,version_node_id) REFERENCES ext_document_version(tenant_id,knowledge_base_id,version_node_id)
);
CREATE TABLE ext_lineage_audit (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, version_node_id TEXT NOT NULL,
    operation_id TEXT NOT NULL UNIQUE, payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64),
    occurred_at TEXT NOT NULL,
    FOREIGN KEY(tenant_id,knowledge_base_id,version_node_id) REFERENCES ext_document_version(tenant_id,knowledge_base_id,version_node_id)
);
CREATE TABLE ext_lineage_edge (
    edge_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    from_node_id TEXT NOT NULL, to_node_id TEXT NOT NULL,
    edge_type TEXT NOT NULL CHECK(edge_type IN ('derived_from','supersedes','merged_into','supplements','amends','interprets','related_to')),
    edge_class TEXT NOT NULL CHECK(
      (edge_type='derived_from' AND edge_class='reconstructable') OR
      (edge_type IN ('supersedes','merged_into','supplements','amends','interprets','related_to') AND edge_class='governance_critical')),
    created_at TEXT NOT NULL,
    CHECK(from_node_id<>to_node_id),
    UNIQUE(tenant_id,knowledge_base_id,from_node_id,to_node_id,edge_type),
    FOREIGN KEY(tenant_id,knowledge_base_id,from_node_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,to_node_id) REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id)
);
CREATE INDEX ext_lineage_edge_source ON ext_lineage_edge(tenant_id,knowledge_base_id,from_node_id,edge_type);
