package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AttachmentSemantic is the deterministic mapping between an extracted
// attachment and its parent document reference. OriginalFilename is never
// replaced by DisplayName.
type AttachmentSemantic struct {
	AttachmentID       string
	TenantID           uint64
	KnowledgeBaseID    string
	ParentDocumentID   string
	ParentVersionID    string
	ParentFileHash     string
	ParentTitle        string
	OriginalFilename   string
	DisplayName        string
	AttachmentSequence int
	ReferenceTitle     string
	ReferenceLocation  string
	ReferenceExcerpt   string
	MappingStatus      string
	MappingConfidence  float64
	IndexPolicy        string
}

// SaveAttachmentSemantics upserts a batch of semantic records. A batch is
// atomic so a failed attachment upload cannot leave half a mapping behind.
func (db *DB) SaveAttachmentSemantics(ctx context.Context, items []AttachmentSemantic) error {
	if len(items) == 0 {
		return nil
	}
	for index := range items {
		if items[index].IndexPolicy == "" {
			items[index].IndexPolicy = "default"
		}
		if err := validateAttachmentSemantic(items[index]); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return db.Write(ctx, func(tx *sql.Tx) error {
		for _, item := range items {
			_, err := tx.ExecContext(ctx, `INSERT INTO ext_attachment_semantics(
				attachment_id, tenant_id, knowledge_base_id, parent_document_id,
				parent_version_id, parent_file_hash, parent_title, original_filename,
				display_name, attachment_sequence, reference_title, reference_location,
				reference_excerpt, mapping_status, mapping_confidence, index_policy, updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(tenant_id, attachment_id) DO UPDATE SET
				knowledge_base_id=excluded.knowledge_base_id,
				parent_document_id=excluded.parent_document_id,
				parent_version_id=excluded.parent_version_id,
				parent_file_hash=excluded.parent_file_hash,
				parent_title=excluded.parent_title,
				original_filename=excluded.original_filename,
				display_name=excluded.display_name,
				attachment_sequence=excluded.attachment_sequence,
				reference_title=excluded.reference_title,
				reference_location=excluded.reference_location,
				reference_excerpt=excluded.reference_excerpt,
				mapping_status=excluded.mapping_status,
				mapping_confidence=excluded.mapping_confidence,
				index_policy=excluded.index_policy,
				updated_at=excluded.updated_at`,
				item.AttachmentID, int64(item.TenantID), item.KnowledgeBaseID,
				item.ParentDocumentID, item.ParentVersionID, item.ParentFileHash,
				item.ParentTitle, item.OriginalFilename, item.DisplayName,
				item.AttachmentSequence, item.ReferenceTitle, item.ReferenceLocation,
				item.ReferenceExcerpt, item.MappingStatus, item.MappingConfidence,
				item.IndexPolicy, now)
			if err != nil {
				return fmt.Errorf("save attachment semantic %s: %w", item.AttachmentID, err)
			}
		}
		return nil
	})
}

// AttachmentSemantics returns records keyed by attachment id for one tenant.
// Knowledge base identity is checked by the caller against the native record.
func (db *DB) AttachmentSemantics(ctx context.Context, tenant uint64, ids []string) (map[string]AttachmentSemantic, error) {
	result := map[string]AttachmentSemantic{}
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, int64(tenant))
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(placeholders) == 0 {
		return result, nil
	}
	err := db.View(ctx, func(tx *sql.Tx) error {
		query := `SELECT attachment_id, tenant_id, knowledge_base_id, parent_document_id,
			parent_version_id, parent_file_hash, parent_title, original_filename,
			display_name, attachment_sequence, reference_title, reference_location,
			reference_excerpt, mapping_status, mapping_confidence, index_policy
			FROM ext_attachment_semantics WHERE tenant_id=? AND attachment_id IN (` + strings.Join(placeholders, ",") + `)`
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AttachmentSemantic
			var tenantValue int64
			if err := rows.Scan(&item.AttachmentID, &tenantValue, &item.KnowledgeBaseID,
				&item.ParentDocumentID, &item.ParentVersionID, &item.ParentFileHash,
				&item.ParentTitle, &item.OriginalFilename, &item.DisplayName,
				&item.AttachmentSequence, &item.ReferenceTitle, &item.ReferenceLocation,
				&item.ReferenceExcerpt, &item.MappingStatus, &item.MappingConfidence,
				&item.IndexPolicy); err != nil {
				return err
			}
			item.TenantID = uint64(tenantValue)
			result[item.AttachmentID] = item
		}
		return rows.Err()
	})
	return result, err
}

func validateAttachmentSemantic(item AttachmentSemantic) error {
	if strings.TrimSpace(item.AttachmentID) == "" {
		return fmt.Errorf("attachment semantic: attachment id is required")
	}
	if strings.TrimSpace(item.KnowledgeBaseID) == "" {
		return fmt.Errorf("attachment semantic %s: knowledge base id is required", item.AttachmentID)
	}
	if strings.TrimSpace(item.ParentDocumentID) == "" {
		return fmt.Errorf("attachment semantic %s: parent document id is required", item.AttachmentID)
	}
	if strings.TrimSpace(item.OriginalFilename) == "" {
		return fmt.Errorf("attachment semantic %s: original filename is required", item.AttachmentID)
	}
	if len(item.ParentFileHash) != 32 && len(item.ParentFileHash) != 64 {
		return fmt.Errorf("attachment semantic %s: parent file hash must be a native or sha256 hash", item.AttachmentID)
	}
	switch item.MappingStatus {
	case "confirmed", "candidate", "unmatched":
	default:
		return fmt.Errorf("attachment semantic %s: invalid mapping status %q", item.AttachmentID, item.MappingStatus)
	}
	if item.MappingConfidence < 0 || item.MappingConfidence > 1 {
		return fmt.Errorf("attachment semantic %s: invalid confidence", item.AttachmentID)
	}
	if item.IndexPolicy == "" {
		item.IndexPolicy = "default"
	}
	return nil
}
