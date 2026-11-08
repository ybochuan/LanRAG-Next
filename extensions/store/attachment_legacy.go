package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

// LegacyAttachmentProof records the bytes checked by the local backfill tool.
// The parent SHA-256 is retained as the original adoption evidence.
type LegacyAttachmentProof struct {
	ParentDocumentID   string `json:"parent_document_id"`
	AttachmentID       string `json:"attachment_id"`
	ParentFileHash     string `json:"parent_file_hash"`
	ParentSHA256       string `json:"parent_sha256"`
	AttachmentSHA256   string `json:"attachment_sha256"`
	AttachmentFileHash string `json:"attachment_file_hash"`
}

// AdoptLegacyAttachments commits an explicitly reviewed, revalidated batch.
// Upload receipts and already scheduled or removed documents cannot be adopted.
func (db *DB) AdoptLegacyAttachments(ctx context.Context, tenant uint64, kb string, proofs []LegacyAttachmentProof) error {
	if tenant == 0 || kb == "" || len(proofs) == 0 {
		return errAttachmentImportConflict
	}
	return db.Write(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, p := range proofs {
			if p.ParentDocumentID == "" || p.AttachmentID == "" || p.ParentDocumentID == p.AttachmentID {
				return errAttachmentImportConflict
			}
			for _, id := range []string{p.ParentDocumentID, p.AttachmentID} {
				if err := checkAttachmentParentWritable(ctx, tx, tenant, kb, id); err != nil {
					return err
				}
			}
			result, err := tx.ExecContext(ctx, `INSERT INTO ext_attachment_links AS a (
				import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,
				attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at,evidence_kind,adoption_parent_sha256
			) VALUES(?,?,?,?,?,?,?,?,1,?,'legacy_reextracted',?)
			ON CONFLICT(tenant_id,knowledge_base_id,parent_document_id,attachment_sha256)
			DO UPDATE SET attachment_id=excluded.attachment_id, parent_file_hash=excluded.parent_file_hash,
				submitted=1, evidence_kind=excluded.evidence_kind, retired_at=NULL,
				adoption_parent_sha256=CASE WHEN a.adoption_parent_sha256='' THEN excluded.adoption_parent_sha256 ELSE a.adoption_parent_sha256 END
			WHERE a.attachment_file_hash=excluded.attachment_file_hash AND a.cleanup_started=0 AND a.removed_at IS NULL
				AND ((a.submitted=0 AND a.attachment_id IS NULL)
				OR (a.evidence_kind='legacy_reextracted' AND a.attachment_id=excluded.attachment_id))`,
				rand.Text(), fmt.Sprint(tenant), kb, p.ParentDocumentID, p.ParentFileHash,
				p.AttachmentSHA256, p.AttachmentFileHash, p.AttachmentID, now, p.ParentSHA256)
			if err := requireAttachmentRow(result, err); err != nil {
				return err
			}
		}
		return nil
	})
}
