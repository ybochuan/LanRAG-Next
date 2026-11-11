package business

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

const (
	documentAvailabilityActive  = "active"
	documentAvailabilityMissing = "missing"
	documentAvailabilityDeleted = "deleted"
)

// refreshFamilyAvailability observes the native presence of the family's
// current members and published head. Native document APIs are authoritative
// for presence; this table is an observable cache used by read projections.
func (s *Service) refreshFamilyAvailability(ctx context.Context, scope provenance.Scope, familyID string) error {
	if s == nil || s.db == nil || !validID(familyID) {
		return nil
	}
	ids := []string{}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document_id FROM ext_business_document_family_members
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?
			UNION
			SELECT document_id FROM ext_business_family_member_history
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?
			UNION
			SELECT document_id FROM ext_business_family_published_heads
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?`,
			append(append(args(scope, familyID), args(scope, familyID)...), args(scope, familyID)...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return s.refreshDocumentAvailability(ctx, scope, ids)
}

func (s *Service) refreshDocumentAvailability(ctx context.Context, scope provenance.Scope, ids []string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	ids = uniqueDocumentIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	native, ok := s.remote.(*NativePublisher)
	if !ok || native == nil || native.client == nil {
		return nil
	}
	docs, err := native.client.AttachmentDocuments(ctx, []string{scope.KnowledgeBaseID}, ids)
	if err != nil {
		// Availability is a read projection. A transient native failure must
		// not turn a document read into a false deletion.
		return nil
	}
	found := make(map[string]bool, len(docs))
	for _, doc := range docs {
		found[doc.ID] = true
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			status := documentAvailabilityMissing
			reason := "native_document_missing"
			if found[id] {
				status = documentAvailabilityActive
				reason = ""
				for _, doc := range docs {
					if doc.ID == id && strings.Contains(strings.ToLower(doc.ParseStatus), "delet") {
						status = documentAvailabilityDeleted
						reason = "native_parse_status_deleted"
					}
				}
			}
			if err := recordDocumentAvailabilityTx(ctx, tx, scope, id, status, reason, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func recordDocumentAvailabilityTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, documentID, status, reason, observedAt string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_document_availability
		(tenant_id,knowledge_base_id,document_id,status,observed_at,reason) VALUES(?,?,?,?,?,?)
		ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET
			status=excluded.status,observed_at=excluded.observed_at,reason=excluded.reason`,
		args(scope, documentID, status, observedAt, reason)...)
	return err
}

func uniqueDocumentIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !validID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
