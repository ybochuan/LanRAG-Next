CREATE UNIQUE INDEX ext_version_scoped_document ON ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id);
CREATE UNIQUE INDEX ext_edge_scoped_id ON ext_lineage_edge(tenant_id,knowledge_base_id,edge_id);
CREATE TABLE ext_supplement_declarations (
    declaration_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0),
    child_version_node_id TEXT NOT NULL, child_event_id TEXT NOT NULL,
    actor_id TEXT NOT NULL, operation_id TEXT NOT NULL,
    payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64),
    status TEXT NOT NULL CHECK(status IN ('active','revoked')), created_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,operation_id),
    UNIQUE(tenant_id,knowledge_base_id,document_id,revision),
    UNIQUE(tenant_id,knowledge_base_id,document_id,declaration_id),
    UNIQUE(tenant_id,knowledge_base_id,document_id,revision,declaration_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,child_version_node_id)
      REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,child_event_id)
      REFERENCES ext_lifecycle_events(tenant_id,knowledge_base_id,document_id,event_id)
);
CREATE TABLE ext_supplement_parents (
    declaration_id TEXT NOT NULL, tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL, parent_document_id TEXT NOT NULL,
    parent_version_node_id TEXT NOT NULL, parent_event_id TEXT NOT NULL,
    CHECK(document_id<>parent_document_id),
    PRIMARY KEY(declaration_id,parent_document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id,knowledge_base_id,document_id,declaration_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,parent_document_id,parent_version_node_id)
      REFERENCES ext_document_version(tenant_id,knowledge_base_id,document_id,version_node_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,parent_document_id,parent_event_id)
      REFERENCES ext_lifecycle_events(tenant_id,knowledge_base_id,document_id,event_id)
);
CREATE TABLE ext_supplement_current (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0), declaration_id TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,revision,declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id,knowledge_base_id,document_id,revision,declaration_id)
);
CREATE TABLE ext_supplement_edge_facts (
    declaration_id TEXT NOT NULL, tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL, parent_document_id TEXT NOT NULL, edge_id TEXT NOT NULL,
    PRIMARY KEY(declaration_id,parent_document_id),
    FOREIGN KEY(declaration_id,parent_document_id) REFERENCES ext_supplement_parents(declaration_id,parent_document_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id,knowledge_base_id,document_id,declaration_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,edge_id) REFERENCES ext_lineage_edge(tenant_id,knowledge_base_id,edge_id)
);
CREATE TABLE ext_supplement_audit (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    declaration_id TEXT NOT NULL UNIQUE, actor_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active','revoked')), occurred_at TEXT NOT NULL,
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id,declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id,knowledge_base_id,document_id,declaration_id)
);
CREATE INDEX ext_supplement_parent_lookup ON ext_supplement_parents(tenant_id,knowledge_base_id,parent_document_id);

-- Review subjects support inherited deadlines without inventing annotations.
CREATE TABLE ext_review_subjects (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);
INSERT INTO ext_review_subjects SELECT DISTINCT tenant_id,knowledge_base_id,document_id FROM ext_review_alerts;
ALTER TABLE ext_review_alerts RENAME TO ext_review_alerts_legacy;
CREATE TABLE ext_review_alerts (
    alert_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL,
    review_due TEXT NOT NULL,
    annotation_revision INTEGER NOT NULL CHECK(annotation_revision>0),
    generation INTEGER NOT NULL CHECK(generation>0),
    status TEXT NOT NULL CHECK(status IN ('open','resolved')),
    detected_at TEXT NOT NULL, delivered_at TEXT, resolved_at TEXT,
    UNIQUE(tenant_id,knowledge_base_id,document_id,review_due),
    FOREIGN KEY(tenant_id,knowledge_base_id,document_id) REFERENCES ext_review_subjects(tenant_id,knowledge_base_id,document_id),
    CHECK((status='open' AND resolved_at IS NULL) OR (status='resolved' AND resolved_at IS NOT NULL))
);
INSERT INTO ext_review_alerts SELECT * FROM ext_review_alerts_legacy;
DROP TABLE ext_review_alerts_legacy;
CREATE UNIQUE INDEX ext_review_one_open ON ext_review_alerts(tenant_id,knowledge_base_id,document_id) WHERE status='open';
CREATE INDEX ext_review_inbox ON ext_review_alerts(tenant_id,knowledge_base_id,status,detected_at,alert_id);
