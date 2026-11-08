CREATE TABLE ext_parser_rendition_bindings (
    identity_key TEXT PRIMARY KEY NOT NULL,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    artifact_node_id TEXT NOT NULL UNIQUE,
    source_version_id TEXT NOT NULL,
    source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),
    artifact_sha256 TEXT NOT NULL CHECK(length(artifact_sha256)=64),
    bound_at TEXT NOT NULL,
    FOREIGN KEY(identity_key) REFERENCES ext_parser_artifacts(identity_key),
    FOREIGN KEY(identity_key,tenant_id,knowledge_base_id,document_id)
        REFERENCES ext_parser_documents(identity_key,tenant_id,knowledge_base_id,doc_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,artifact_node_id)
        REFERENCES ext_lineage_node(tenant_id,knowledge_base_id,node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,source_version_id)
        REFERENCES ext_source_snapshot_evidence(tenant_id,knowledge_base_id,version_node_id)
);

CREATE INDEX ext_parser_rendition_source
    ON ext_parser_rendition_bindings(tenant_id,knowledge_base_id,document_id,source_version_id);

CREATE VIEW ext_rendition_edges AS
SELECT 'rendition:'||artifact_node_id AS edge_id,tenant_id,knowledge_base_id,
       artifact_node_id AS from_node_id,source_version_id AS to_node_id,
       'rendition_of' AS edge_type,bound_at AS created_at
FROM ext_parser_rendition_bindings;
