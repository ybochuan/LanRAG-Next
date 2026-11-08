CREATE TABLE ext_attachment_links (
    import_id TEXT PRIMARY KEY NOT NULL,
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    parent_document_id TEXT NOT NULL,
    parent_file_hash TEXT NOT NULL CHECK(length(parent_file_hash)=32),
    attachment_sha256 TEXT NOT NULL CHECK(length(attachment_sha256)=64),
    attachment_file_hash TEXT NOT NULL CHECK(length(attachment_file_hash)=32),
    attachment_id TEXT,
    submitted INTEGER NOT NULL DEFAULT 0 CHECK(submitted IN (0,1)),
    created_at TEXT NOT NULL,
    CHECK(attachment_id IS NULL OR (attachment_id<>parent_document_id AND submitted=1)),
    UNIQUE(tenant_id,knowledge_base_id,parent_document_id,attachment_sha256),
    UNIQUE(tenant_id,attachment_id)
);
