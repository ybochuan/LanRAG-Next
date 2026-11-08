ALTER TABLE ext_attachment_links ADD COLUMN evidence_kind TEXT NOT NULL DEFAULT 'upload_receipt'
    CHECK(evidence_kind IN ('upload_receipt','legacy_reextracted'));
ALTER TABLE ext_attachment_links ADD COLUMN retired_at TEXT;
ALTER TABLE ext_attachment_links ADD COLUMN adoption_parent_sha256 TEXT NOT NULL DEFAULT ''
    CHECK((evidence_kind='upload_receipt' AND adoption_parent_sha256='')
       OR (evidence_kind='legacy_reextracted' AND length(adoption_parent_sha256)=64));
