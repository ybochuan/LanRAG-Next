ALTER TABLE ext_attachment_links ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'extracted' CHECK(source_kind IN ('extracted','uploaded'));
ALTER TABLE ext_attachment_links ADD COLUMN detached_at TEXT;

CREATE TABLE ext_attachment_confirmations (
 tenant_id TEXT NOT NULL,
 knowledge_base_id TEXT NOT NULL,
 parent_document_id TEXT NOT NULL,
 parent_file_hash TEXT NOT NULL,
 attachment_id TEXT NOT NULL,
 attachment_file_hash TEXT NOT NULL,
 revision INTEGER NOT NULL,
 reference_title TEXT NOT NULL,
 reference_location TEXT NOT NULL,
 reference_excerpt TEXT NOT NULL,
 reference_sequence INTEGER NOT NULL,
 PRIMARY KEY(tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_id,attachment_file_hash)
);

CREATE TABLE ext_attachment_title_updates (
 tenant_id TEXT NOT NULL,
 knowledge_base_id TEXT NOT NULL,
 operation_id TEXT NOT NULL,
 document_id TEXT NOT NULL,
 file_hash TEXT NOT NULL,
 expected_title TEXT NOT NULL,
 title TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','synced','conflict')),
 PRIMARY KEY(tenant_id,knowledge_base_id,operation_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,operation_id) REFERENCES ext_business_receipts(tenant_id,knowledge_base_id,operation_id)
);
CREATE INDEX idx_attachment_titles_pending ON ext_attachment_title_updates(status,tenant_id,knowledge_base_id,document_id);

CREATE TABLE ext_attachment_reclaims (
 tenant_id TEXT NOT NULL,
 knowledge_base_id TEXT NOT NULL,
 document_id TEXT NOT NULL,
 parent_document_id TEXT NOT NULL,
 file_hash TEXT NOT NULL,
 title TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'deleting' CHECK(status IN ('deleting','pending','removed','shared')),
 next_attempt INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);
CREATE INDEX idx_attachment_reclaims_due ON ext_attachment_reclaims(tenant_id,next_attempt) WHERE status IN ('deleting','pending');
