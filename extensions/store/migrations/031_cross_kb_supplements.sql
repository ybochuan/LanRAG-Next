CREATE TABLE ext_supplement_cross_parents (
    declaration_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    parent_knowledge_base_id TEXT NOT NULL,
    parent_document_id TEXT NOT NULL,
    parent_version_node_id TEXT NOT NULL,
    parent_event_id TEXT NOT NULL,
    CHECK(document_id <> parent_document_id OR knowledge_base_id <> parent_knowledge_base_id),
    PRIMARY KEY(declaration_id, parent_knowledge_base_id, parent_document_id),
    FOREIGN KEY(tenant_id, knowledge_base_id, document_id, declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id, knowledge_base_id, document_id, declaration_id),
    FOREIGN KEY(tenant_id, parent_knowledge_base_id, parent_document_id, parent_version_node_id)
      REFERENCES ext_document_version(tenant_id, knowledge_base_id, document_id, version_node_id),
    FOREIGN KEY(tenant_id, parent_knowledge_base_id, parent_document_id, parent_event_id)
      REFERENCES ext_lifecycle_events(tenant_id, knowledge_base_id, document_id, event_id)
);
CREATE INDEX ext_supplement_cross_parent_lookup
    ON ext_supplement_cross_parents(tenant_id, parent_knowledge_base_id, parent_document_id);

CREATE TABLE ext_supplement_cross_edges (
    edge_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL,
    source_knowledge_base_id TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_knowledge_base_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    edge_type TEXT NOT NULL CHECK(edge_type = 'supplements'),
    edge_class TEXT NOT NULL CHECK(edge_class = 'governance_critical'),
    created_at TEXT NOT NULL,
    UNIQUE(tenant_id, source_knowledge_base_id, source_node_id,
           target_knowledge_base_id, target_node_id, edge_type),
    FOREIGN KEY(tenant_id, source_knowledge_base_id, source_node_id)
      REFERENCES ext_lineage_node(tenant_id, knowledge_base_id, node_id),
    FOREIGN KEY(tenant_id, target_knowledge_base_id, target_node_id)
      REFERENCES ext_lineage_node(tenant_id, knowledge_base_id, node_id)
);
CREATE INDEX ext_supplement_cross_edges_source
    ON ext_supplement_cross_edges(tenant_id, source_knowledge_base_id, source_node_id);
CREATE INDEX ext_supplement_cross_edges_target
    ON ext_supplement_cross_edges(tenant_id, target_knowledge_base_id, target_node_id);

CREATE TABLE ext_supplement_cross_edge_facts (
    declaration_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    parent_knowledge_base_id TEXT NOT NULL,
    parent_document_id TEXT NOT NULL,
    cross_edge_id TEXT NOT NULL,
    PRIMARY KEY(declaration_id, parent_knowledge_base_id, parent_document_id),
    FOREIGN KEY(declaration_id, parent_knowledge_base_id, parent_document_id)
      REFERENCES ext_supplement_cross_parents(declaration_id, parent_knowledge_base_id, parent_document_id),
    FOREIGN KEY(tenant_id, knowledge_base_id, document_id, declaration_id)
      REFERENCES ext_supplement_declarations(tenant_id, knowledge_base_id, document_id, declaration_id),
    FOREIGN KEY(cross_edge_id) REFERENCES ext_supplement_cross_edges(edge_id)
);
