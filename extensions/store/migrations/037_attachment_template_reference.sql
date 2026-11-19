ALTER TABLE ext_document_relations ADD COLUMN attachment_reference_json TEXT;
ALTER TABLE ext_attachment_reclaims ADD COLUMN resources TEXT NOT NULL DEFAULT '';
ALTER TABLE ext_attachment_reclaims ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE ext_attachment_reclaims ADD COLUMN indexes_cleared INTEGER NOT NULL DEFAULT 0 CHECK(indexes_cleared IN (0,1));
ALTER TABLE ext_attachment_delete_intents ADD COLUMN reservation_id TEXT NOT NULL DEFAULT '';
