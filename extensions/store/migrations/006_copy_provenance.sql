CREATE TABLE ext_provenance_sources (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    version_id TEXT NOT NULL, content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    native_sequence INTEGER NOT NULL CHECK(native_sequence>0),
    lifecycle_event_id TEXT NOT NULL, evidence_hash TEXT NOT NULL CHECK(length(evidence_hash)=64),
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id,version_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,lifecycle_event_id)
      REFERENCES ext_lifecycle_events(tenant_id,knowledge_base_id,document_id,event_id)
);
CREATE TABLE ext_provenance_source_heads (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0), version_id TEXT NOT NULL,
    native_sequence INTEGER NOT NULL CHECK(native_sequence>0),
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,version_id)
      REFERENCES ext_provenance_sources(tenant_id,knowledge_base_id,document_id,version_id)
);
CREATE TABLE ext_provenance_manifests (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, copy_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0), status TEXT NOT NULL CHECK(status IN ('pending','ready')),
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,copy_id,revision)
);
CREATE TABLE ext_provenance_copy_heads (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, copy_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0),
    PRIMARY KEY(tenant_id,knowledge_base_id,copy_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,copy_id,revision)
      REFERENCES ext_provenance_manifests(tenant_id,knowledge_base_id,copy_id,revision)
);
CREATE TABLE ext_provenance_manifest_sources (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, copy_id TEXT NOT NULL,
    revision INTEGER NOT NULL, document_id TEXT NOT NULL, version_id TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,copy_id,revision,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,copy_id,revision)
      REFERENCES ext_provenance_manifests(tenant_id,knowledge_base_id,copy_id,revision),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,version_id)
      REFERENCES ext_provenance_sources(tenant_id,knowledge_base_id,document_id,version_id)
);
CREATE TABLE ext_provenance_manifest_parents (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, copy_id TEXT NOT NULL,
    revision INTEGER NOT NULL, parent_copy_id TEXT NOT NULL, parent_revision INTEGER NOT NULL,
    CHECK(copy_id<>parent_copy_id),
    PRIMARY KEY(tenant_id,knowledge_base_id,copy_id,revision,parent_copy_id,parent_revision),
    FOREIGN KEY(tenant_id,knowledge_base_id,copy_id,revision)
      REFERENCES ext_provenance_manifests(tenant_id,knowledge_base_id,copy_id,revision),
    FOREIGN KEY(tenant_id,knowledge_base_id,parent_copy_id,parent_revision)
      REFERENCES ext_provenance_manifests(tenant_id,knowledge_base_id,copy_id,revision)
);
CREATE TABLE ext_provenance_receipts (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, operation_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('source','copy')), payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64),
    result_json TEXT NOT NULL CHECK(json_valid(result_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,operation_id)
);
CREATE TABLE ext_provenance_audit (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, operation_id TEXT NOT NULL,
    aggregate_id TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
    status TEXT NOT NULL, occurred_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,operation_id)
      REFERENCES ext_provenance_receipts(tenant_id,knowledge_base_id,operation_id)
);
