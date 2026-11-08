CREATE TABLE ext_attachment_delete_intents (
    tenant_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,document_id)
);

ALTER TABLE ext_attachment_links ADD COLUMN cleanup_started INTEGER NOT NULL DEFAULT 0 CHECK(cleanup_started IN (0,1));
ALTER TABLE ext_attachment_links ADD COLUMN cleanup_next_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ext_attachment_links ADD COLUMN cleanup_error TEXT NOT NULL DEFAULT '';
ALTER TABLE ext_attachment_links ADD COLUMN removed_at TEXT;

CREATE INDEX ext_attachment_cleanup_due ON ext_attachment_links(tenant_id,knowledge_base_id,cleanup_next_at)
    WHERE attachment_id IS NOT NULL AND removed_at IS NULL;
