CREATE TABLE IF NOT EXISTS ext_attachment_semantics (
    attachment_id TEXT NOT NULL,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    parent_document_id TEXT NOT NULL,
    parent_version_id TEXT NOT NULL DEFAULT '',
    parent_file_hash TEXT NOT NULL,
    parent_title TEXT NOT NULL DEFAULT '',
    original_filename TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    attachment_sequence INTEGER NOT NULL DEFAULT 0,
    reference_title TEXT NOT NULL DEFAULT '',
    reference_location TEXT NOT NULL DEFAULT '',
    reference_excerpt TEXT NOT NULL DEFAULT '',
    mapping_status TEXT NOT NULL DEFAULT 'unmatched',
    mapping_confidence REAL NOT NULL DEFAULT 0,
    index_policy TEXT NOT NULL DEFAULT 'default',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, attachment_id)
);

CREATE INDEX IF NOT EXISTS idx_ext_attachment_semantics_parent
    ON ext_attachment_semantics(tenant_id, parent_document_id);
CREATE INDEX IF NOT EXISTS idx_ext_attachment_semantics_kb
    ON ext_attachment_semantics(tenant_id, knowledge_base_id);
