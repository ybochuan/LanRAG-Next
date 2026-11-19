package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrAttachmentConflict indicates a stale operation or an unverified attachment.
var ErrAttachmentConflict = errors.New("attachment revision conflict")

// AttachmentPresentationRevisionSQL accepts only proved title/folder changes.
// Both arguments are trusted SQL column expressions (or bound placeholders).
func AttachmentPresentationRevisionSQL(oldVersion, newVersion string) string {
	return `EXISTS (SELECT 1 FROM ext_source_snapshot_evidence old_attachment_evidence
		JOIN ext_source_snapshot_evidence new_attachment_evidence
		ON new_attachment_evidence.tenant_id=old_attachment_evidence.tenant_id
		AND new_attachment_evidence.knowledge_base_id=old_attachment_evidence.knowledge_base_id
		WHERE old_attachment_evidence.version_node_id=` + oldVersion + `
		AND new_attachment_evidence.version_node_id=` + newVersion + `
		AND json_extract(old_attachment_evidence.snapshot_json,'$.document_id')=json_extract(new_attachment_evidence.snapshot_json,'$.document_id')
		AND json_extract(old_attachment_evidence.snapshot_json,'$.chunks')=json_extract(new_attachment_evidence.snapshot_json,'$.chunks')
		AND json_remove(old_attachment_evidence.snapshot_json,'$.header_fields.title','$.header_fields.folder_path',
			'$.header_hash','$.root_hash')=json_remove(new_attachment_evidence.snapshot_json,'$.header_fields.title',
			'$.header_fields.folder_path','$.header_hash','$.root_hash'))`
}

// AttachmentReference is the evidence saved by an operator for one relation.
type AttachmentReference struct {
	Title    string `json:"title,omitempty"`
	Location string `json:"location,omitempty"`
	Excerpt  string `json:"excerpt,omitempty"`
	Sequence int    `json:"sequence,omitempty"`
}

// AttachmentConfirmation is meaningful only for the specified native revisions.
type AttachmentConfirmation struct {
	AttachmentReference
	ParentID, ParentHash, AttachmentID, AttachmentHash string
	KnowledgeBaseID                                    string
	Revision                                           int64
}

// AttachmentConfirmations loads version-bound decisions, newest decision first.
func (db *DB) AttachmentConfirmations(ctx context.Context, tenant uint64, ids []string) ([]AttachmentConfirmation, error) {
	var result []AttachmentConfirmation
	keys, _ := json.Marshal(ids)
	err := db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT knowledge_base_id,parent_document_id,parent_file_hash,attachment_id,attachment_file_hash,
			revision,reference_title,reference_location,reference_excerpt,reference_sequence
			FROM ext_attachment_confirmations WHERE tenant_id=? AND attachment_id IN (SELECT value FROM json_each(?))`,
			fmt.Sprint(tenant), string(keys))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a AttachmentConfirmation
			if err := rows.Scan(&a.KnowledgeBaseID, &a.ParentID, &a.ParentHash, &a.AttachmentID, &a.AttachmentHash, &a.Revision,
				&a.Title, &a.Location, &a.Excerpt, &a.Sequence); err != nil {
				return err
			}
			result = append(result, a)
		}
		return rows.Err()
	})
	return result, err
}

// AttachmentReviewEdit carries verified native identities, version and title expectations.
type AttachmentReviewEdit struct {
	Tenant                                               uint64
	KB, Actor, Operation, Action                         string
	Parent, ParentHash, Document, DocumentHash, ImportID string
	PreviousTitle, NextTitle                             string
	NativeTitle                                          string `json:"-"`
	ExpectedRevision                                     int64
	Reference                                            AttachmentReference
}

type AttachmentReviewResult struct {
	Revision   int64  `json:"revision"`
	TitleState string `json:"title_state,omitempty"`
}

// AttachmentReview saves a confirmation/detach/restore and its audit atomically.
func (db *DB) AttachmentReview(ctx context.Context, c AttachmentReviewEdit) (AttachmentReviewResult, error) {
	var result AttachmentReviewResult
	tenant := fmt.Sprint(c.Tenant)
	payload, _ := json.Marshal(c)
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := db.Write(ctx, func(tx *sql.Tx) error {
		var hash, response string
		err := tx.QueryRowContext(ctx, `SELECT payload_hash,result_json FROM ext_business_receipts
			WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?`, tenant, c.KB, c.Operation).Scan(&hash, &response)
		if err == nil {
			if hash != digest {
				return ErrAttachmentConflict
			}
			return json.Unmarshal([]byte(response), &result)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = checkAttachmentParentWritable(ctx, tx, c.Tenant, c.KB, c.Parent); err != nil {
			return ErrAttachmentConflict
		}
		if err = checkAttachmentParentWritable(ctx, tx, c.Tenant, c.KB, c.Document); err != nil {
			return ErrAttachmentConflict
		}
		args := []any{tenant, c.KB, c.Parent, c.ParentHash, c.Document, c.DocumentHash}
		var detached, retired bool
		err = tx.QueryRowContext(ctx, `SELECT detached_at IS NOT NULL,retired_at IS NOT NULL FROM ext_attachment_links
			WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND parent_file_hash=?
			AND attachment_id=? AND attachment_file_hash=? AND removed_at IS NULL AND cleanup_started=0
			AND ((evidence_kind='upload_receipt' AND import_id=?) OR (evidence_kind='legacy_reextracted' AND ?=''))`,
			append(append([]any{}, args...), c.ImportID, c.ImportID)...).Scan(&detached, &retired)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAttachmentConflict
		}
		if err != nil {
			return err
		}
		if c.Action == "restore" && !detached || c.Action != "restore" && (detached || retired) {
			return ErrAttachmentConflict
		}
		previous := AttachmentConfirmation{}
		err = tx.QueryRowContext(ctx, `SELECT revision,reference_title,reference_location,reference_excerpt,reference_sequence
			FROM ext_attachment_confirmations WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=?
			AND parent_file_hash=? AND attachment_id=? AND attachment_file_hash=?`, args...).
			Scan(&previous.Revision, &previous.Title, &previous.Location, &previous.Excerpt, &previous.Sequence)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if previous.Revision != c.ExpectedRevision {
			return ErrAttachmentConflict
		}
		if c.NextTitle != "" && c.PreviousTitle != c.NativeTitle {
			return ErrAttachmentConflict
		}
		result.Revision = previous.Revision
		switch c.Action {
		case "confirm":
			result.Revision++
			values := append(append([]any{}, args...), result.Revision, c.Reference.Title, c.Reference.Location, c.Reference.Excerpt, c.Reference.Sequence)
			_, err = tx.ExecContext(ctx, `INSERT INTO ext_attachment_confirmations VALUES(?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_id,attachment_file_hash)
				DO UPDATE SET revision=excluded.revision, reference_title=excluded.reference_title,
				reference_location=excluded.reference_location,reference_excerpt=excluded.reference_excerpt,reference_sequence=excluded.reference_sequence`, values...)
		case "detach":
			_, err = tx.ExecContext(ctx, `UPDATE ext_attachment_links SET detached_at=?,retired_at=?
				WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND parent_file_hash=?
				AND attachment_id=? AND attachment_file_hash=?`, append([]any{now, now}, args...)...)
		case "restore":
			_, err = tx.ExecContext(ctx, `UPDATE ext_attachment_links SET detached_at=NULL,retired_at=NULL
				WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND parent_file_hash=?
				AND attachment_id=? AND attachment_file_hash=?`, args...)
		default:
			return ErrAttachmentConflict
		}
		if err != nil {
			return err
		}
		changeTitle := c.NextTitle != "" && c.PreviousTitle != c.NextTitle
		if changeTitle {
			result.TitleState = "pending"
		}
		saved, _ := json.Marshal(result)
		if _, err = tx.ExecContext(ctx, `INSERT INTO ext_business_receipts VALUES(?,?,?,?,?)`, tenant, c.KB, c.Operation, digest, string(saved)); err != nil {
			return err
		}
		audit, _ := json.Marshal(map[string]any{"before": previous.AttachmentReference, "after": c.Reference, "previous_title": c.PreviousTitle, "title": c.NextTitle,
			"parent_document_id": c.Parent, "parent_file_hash": c.ParentHash, "attachment_file_hash": c.DocumentHash})
		if _, err = tx.ExecContext(ctx, `INSERT INTO ext_business_audit(tenant_id,knowledge_base_id,operation_id,actor_id,action,aggregate_id,revision,status,occurred_at,payload_json)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, tenant, c.KB, c.Operation, c.Actor, "attachment_"+c.Action, c.Document, result.Revision, "applied", now, string(audit)); err != nil {
			return err
		}
		if changeTitle {
			// Supersede pending requests for this native title in the same transaction.
			if _, err = tx.ExecContext(ctx, `UPDATE ext_attachment_title_updates SET status='conflict'
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status='pending'`, tenant, c.KB, c.Document); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO ext_attachment_title_updates VALUES(?,?,?,?,?,?,?,'pending')`,
				tenant, c.KB, c.Operation, c.Document, c.DocumentHash, c.PreviousTitle, c.NextTitle)
		}
		return err
	})
	return result, err
}

// AttachmentTitleUpdate is a durable projection to the title in the native KB.
type AttachmentTitleUpdate struct {
	KB, Operation, Document, Version, Previous, Title string
}

func (db *DB) PendingAttachmentTitles(ctx context.Context, tenant uint64) ([]AttachmentTitleUpdate, error) {
	var result []AttachmentTitleUpdate
	err := db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT knowledge_base_id,operation_id,document_id,file_hash,expected_title,title
			FROM ext_attachment_title_updates WHERE tenant_id=? AND status='pending' ORDER BY rowid LIMIT 32`, fmt.Sprint(tenant))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AttachmentTitleUpdate
			if err := rows.Scan(&item.KB, &item.Operation, &item.Document, &item.Version, &item.Previous, &item.Title); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (db *DB) AttachmentTitleStatus(ctx context.Context, tenant uint64, kb, doc string) (string, error) {
	var result string
	err := db.View(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT status FROM ext_attachment_title_updates WHERE tenant_id=? AND knowledge_base_id=?
			AND document_id=? ORDER BY rowid DESC LIMIT 1`, fmt.Sprint(tenant), kb, doc).Scan(&result)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return result, err
}

func (db *DB) CompleteAttachmentTitle(ctx context.Context, tenant uint64, item AttachmentTitleUpdate, status string) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_title_updates SET status=?
			WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=? AND status='pending'`,
			status, fmt.Sprint(tenant), item.KB, item.Operation)
		return err
	})
}
