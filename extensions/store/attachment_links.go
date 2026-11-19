package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var errAttachmentImportConflict = errors.New("attachment import already submitted or ownership conflicts")

// CancelRejectedAttachmentUpload releases a claim after a definite native rejection.
func (db *DB) CancelRejectedAttachmentUpload(ctx context.Context, importID string) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET submitted=0
			WHERE import_id=? AND submitted=1 AND attachment_id IS NULL AND cleanup_started=0`, importID)
		return requireAttachmentRow(result, err)
	})
}

const (
	AttachmentUploadEvidence = "upload_receipt"
	AttachmentLegacyEvidence = "legacy_reextracted"
)

// AttachmentBinding records the native identity verified against an upload receipt.
type AttachmentBinding struct {
	ImportID     string
	AttachmentID string
}

// PrepareAttachmentImport commits a stable receipt before any native upload.
// The same content can be reused when the same main document is reprocessed.
func (db *DB) PrepareAttachmentImport(ctx context.Context, tenant uint64, kb, parent, parentHash, sha, fileHash string) (string, error) {
	id := rand.Text()
	err := db.Write(ctx, func(tx *sql.Tx) error {
		scope := []any{fmt.Sprint(tenant), kb, parent, sha}
		if err := checkAttachmentParentWritable(ctx, tx, tenant, kb, parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_attachment_links
		(tenant_id,knowledge_base_id,parent_document_id,attachment_sha256,import_id,parent_file_hash,attachment_file_hash,created_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,parent_document_id,attachment_sha256) DO UPDATE SET
			import_id=excluded.import_id,parent_file_hash=excluded.parent_file_hash,attachment_file_hash=excluded.attachment_file_hash,
			created_at=excluded.created_at,attachment_id=NULL,submitted=0,cleanup_started=0,cleanup_next_at=0,cleanup_error='',
			retired_at=NULL,removed_at=NULL,detached_at=NULL,evidence_kind='upload_receipt',adoption_parent_sha256='',source_kind='extracted'
			WHERE ext_attachment_links.removed_at IS NOT NULL AND ext_attachment_links.parent_file_hash<>excluded.parent_file_hash`,
			append(scope, id, parentHash, fileHash, time.Now().UTC().Format(time.RFC3339Nano))...); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT import_id FROM ext_attachment_links
		WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND attachment_sha256=? AND attachment_file_hash=?`,
			append(scope, fileHash)...).Scan(&id)
	})
	return id, err
}

// ClaimAttachmentUpload reserves the single allowed POST for this receipt.
func (db *DB) ClaimAttachmentUpload(ctx context.Context, importID string) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links AS a SET submitted=1
		WHERE import_id=? AND submitted=0 AND NOT EXISTS (SELECT 1 FROM ext_attachment_delete_intents d
		WHERE d.tenant_id=a.tenant_id AND d.knowledge_base_id=a.knowledge_base_id AND d.document_id=a.parent_document_id)`, importID)
		return requireAttachmentRow(result, err)
	})
}

// BindAttachments records confirmed child IDs for one verified parent revision.
func (db *DB) BindAttachments(ctx context.Context, tenant uint64, kb, parent, parentHash string, bindings []AttachmentBinding) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := checkAttachmentParentWritable(ctx, tx, tenant, kb, parent); err != nil {
			return err
		}
		return bindAttachmentsTx(ctx, tx, tenant, kb, parent, parentHash, bindings)
	})
}

func bindAttachmentsTx(ctx context.Context, tx *sql.Tx, tenant uint64, kb, parent, parentHash string, bindings []AttachmentBinding) error {
	for _, b := range bindings {
		result, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET attachment_id=?, parent_file_hash=?, retired_at=NULL,detached_at=NULL
		WHERE import_id=? AND tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND submitted=1
		AND cleanup_started=0 AND removed_at IS NULL AND (detached_at IS NULL OR parent_file_hash<>?) AND evidence_kind='upload_receipt'
		AND (attachment_id IS NULL OR attachment_id=?)`, b.AttachmentID, parentHash, b.ImportID, fmt.Sprint(tenant), kb, parent, parentHash, b.AttachmentID)
		if err := requireAttachmentRow(result, err); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileAttachmentManifest switches the visible attachment set only after
// an entire supported parent revision was extracted and its uploads resolved.
// Retired rows remain owned by the parent and can be reused if they return.
func (db *DB) ReconcileAttachmentManifest(ctx context.Context, tenant uint64, kb, parent, parentHash string, bindings []AttachmentBinding, observedIDs []string) error {
	ids, _ := json.Marshal(observedIDs)
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := checkAttachmentParentWritable(ctx, tx, tenant, kb, parent); err != nil {
			return err
		}
		if err := bindAttachmentsTx(ctx, tx, tenant, kb, parent, parentHash, bindings); err != nil {
			return err
		}
		scope := []any{fmt.Sprint(tenant), kb, parent, string(ids)}
		if _, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET parent_file_hash=?,retired_at=NULL
		WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND evidence_kind='legacy_reextracted'
		AND cleanup_started=0 AND removed_at IS NULL AND detached_at IS NULL AND attachment_id IN (SELECT value FROM json_each(?))`,
			append([]any{parentHash}, scope...)...); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET retired_at=?
		WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND attachment_id IS NOT NULL
		AND removed_at IS NULL AND retired_at IS NULL
		AND attachment_id NOT IN (SELECT value FROM json_each(?) WHERE value IS NOT NULL)
		AND (source_kind='extracted' OR parent_file_hash<>?)`,
			append(append([]any{time.Now().UTC().Format(time.RFC3339Nano)}, scope...), parentHash)...)
		return err
	})
}

func checkAttachmentParentWritable(ctx context.Context, tx *sql.Tx, tenant uint64, kb, parent string) error {
	var deleting bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_attachment_delete_intents
		WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?)`, fmt.Sprint(tenant), kb, parent).Scan(&deleting); err != nil {
		return err
	}
	if deleting {
		return errAttachmentImportConflict
	}
	return nil
}

func requireAttachmentRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errAttachmentImportConflict
	}
	return nil
}

// AttachmentOwnership describes the verified owner and the two native file revisions.
type AttachmentOwnership struct {
	ImportID, KnowledgeBaseID, ParentDocumentID, ParentFileHash, AttachmentFileHash string
	EvidenceKind, AttachmentSHA256                                                  string
	Historical                                                                      bool
}

// AttachmentImportState returns the records the importer may reuse or skip.
func (db *DB) AttachmentImportState(ctx context.Context, tenant uint64, kb, parent, version string) (map[string]string, error) {
	result := map[string]string{}
	err := db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT attachment_sha256,CASE
			WHEN parent_file_hash=? AND (detached_at IS NOT NULL OR removed_at IS NOT NULL OR EXISTS(
				SELECT 1 FROM ext_attachment_delete_intents d WHERE d.tenant_id=a.tenant_id AND d.knowledge_base_id=a.knowledge_base_id
				AND d.document_id=a.attachment_id)) THEN 'removed'
			WHEN removed_at IS NOT NULL THEN 'new' WHEN attachment_id IS NOT NULL THEN 'existing' ELSE 'pending' END
			FROM ext_attachment_links a WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=?`, version, fmt.Sprint(tenant), kb, parent)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var hash, state string
			if err := rows.Scan(&hash, &state); err != nil {
				return err
			}
			result[hash] = state
		}
		return rows.Err()
	})
	return result, err
}

// BindManualAttachment preserves provenance for independently uploaded files.
func (db *DB) BindManualAttachment(ctx context.Context, tenant uint64, kb, parent, parentHash string, binding AttachmentBinding) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := checkAttachmentParentWritable(ctx, tx, tenant, kb, parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE ext_attachment_links SET source_kind='uploaded' WHERE import_id=? AND attachment_id IS NULL", binding.ImportID); err != nil {
			return err
		}
		return bindAttachmentsTx(ctx, tx, tenant, kb, parent, parentHash, []AttachmentBinding{binding})
	})
}

// AttachmentOwnershipsTx loads confirmed ownership for requested native IDs.
func AttachmentOwnershipsTx(ctx context.Context, tx *sql.Tx, tenant uint64, ids []string) (map[string]AttachmentOwnership, error) {
	return attachmentOwnershipsTx(ctx, tx, tenant, ids, "evidence_kind, retired_at IS NOT NULL OR removed_at IS NOT NULL")
}

// AttachmentOwnershipsBeforeRevisionsTx supports read-only audit of schema 15 and 16.
func AttachmentOwnershipsBeforeRevisionsTx(ctx context.Context, tx *sql.Tx, tenant uint64, ids []string) (map[string]AttachmentOwnership, error) {
	return attachmentOwnershipsTx(ctx, tx, tenant, ids, "'upload_receipt', 0")
}

func attachmentOwnershipsTx(ctx context.Context, tx *sql.Tx, tenant uint64, ids []string, columns string) (map[string]AttachmentOwnership, error) {
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT attachment_id,import_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_file_hash,attachment_sha256,`+columns+`
	FROM ext_attachment_links WHERE tenant_id=? AND attachment_id IN (SELECT value FROM json_each(?))`, fmt.Sprint(tenant), string(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]AttachmentOwnership)
	for rows.Next() {
		var id string
		var v AttachmentOwnership
		if err := rows.Scan(&id, &v.ImportID, &v.KnowledgeBaseID, &v.ParentDocumentID, &v.ParentFileHash, &v.AttachmentFileHash, &v.AttachmentSHA256, &v.EvidenceKind, &v.Historical); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}
