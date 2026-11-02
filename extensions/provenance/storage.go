package provenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func id(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func digest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && value == strings.ToLower(value)
}
func validScope(s Scope) bool { return s.TenantID != 0 && id(s.KnowledgeBaseID) }
func validBinding(b Binding) bool {
	return id(b.UserID) && id(b.SessionID) && id(b.ResourceID) && id(b.ContentVersion) && digest(b.SHA256)
}
func args(s Scope, tail ...any) []any {
	return append([]any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID}, tail...)
}
func payloadHash(kind string, value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(append([]byte(kind+":"), data...))
	return hex.EncodeToString(sum[:])
}
func lookupReceipt(ctx context.Context, tx *sql.Tx, s Scope, op, kind, hash string) (Receipt, bool, error) {
	var r Receipt
	var storedKind, storedHash string
	var body []byte
	err := tx.QueryRowContext(ctx, "SELECT kind,payload_hash,result_json FROM ext_provenance_receipts WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?", args(s, op)...).Scan(&storedKind, &storedHash, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if storedKind != kind || storedHash != hash {
		return r, true, ErrConflict
	}
	if json.Unmarshal(body, &r) != nil || r.Revision <= 0 || r.Kind != kind {
		return r, true, ErrUnavailable
	}
	return r, true, nil
}
func (l *Ledger) replay(ctx context.Context, s Scope, op, kind, hash string) (r Receipt, exists bool, err error) {
	if l == nil || l.db == nil {
		return r, false, ErrUnavailable
	}
	err = l.db.View(ctx, func(tx *sql.Tx) error {
		var e error
		r, exists, e = lookupReceipt(ctx, tx, s, op, kind, hash)
		return e
	})
	return
}
func (l *Ledger) receipt(ctx context.Context, tx *sql.Tx, s Scope, op, hash string, r Receipt) error {
	body, _ := json.Marshal(r)
	if _, err := tx.ExecContext(ctx, "INSERT INTO ext_provenance_receipts VALUES(?,?,?,?,?,?)", args(s, op, r.Kind, hash, string(body))...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO ext_provenance_audit(tenant_id,knowledge_base_id,operation_id,aggregate_id,revision,status,occurred_at) VALUES(?,?,?,?,?,?,?)", args(s, op, r.ID, r.Revision, r.Status, l.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))...)
	return err
}
func loadManifest(ctx context.Context, tx *sql.Tx, s Scope, copyID string, revision int64) (Manifest, error) {
	var m Manifest
	var body []byte
	if revision == 0 {
		err := tx.QueryRowContext(ctx, `SELECT m.payload_json FROM ext_provenance_copy_heads h JOIN ext_provenance_manifests m ON m.tenant_id=h.tenant_id AND m.knowledge_base_id=h.knowledge_base_id AND m.copy_id=h.copy_id AND m.revision=h.revision WHERE h.tenant_id=? AND h.knowledge_base_id=? AND h.copy_id=?`, args(s, copyID)...).Scan(&body)
		if err != nil {
			return m, err
		}
	} else {
		if err := tx.QueryRowContext(ctx, "SELECT payload_json FROM ext_provenance_manifests WHERE tenant_id=? AND knowledge_base_id=? AND copy_id=? AND revision=?", args(s, copyID, revision)...).Scan(&body); err != nil {
			return m, err
		}
	}
	if json.Unmarshal(body, &m) != nil || m.Scope != s || m.CopyID != copyID || m.Revision <= 0 || !validBinding(m.Binding) {
		return m, ErrUnavailable
	}
	return m, nil
}
func sourceUnion(parts ...[]SourceRef) ([]SourceRef, error) {
	byDoc := map[string]string{}
	for _, part := range parts {
		for _, ref := range part {
			if !id(ref.DocumentID) || !id(ref.VersionID) {
				return nil, ErrEvidence
			}
			if v, ok := byDoc[ref.DocumentID]; ok && v != ref.VersionID {
				return nil, ErrConflict
			}
			byDoc[ref.DocumentID] = ref.VersionID
			if len(byDoc) > 256 {
				return nil, ErrEvidence
			}
		}
	}
	result := make([]SourceRef, 0, len(byDoc))
	for doc, version := range byDoc {
		result = append(result, SourceRef{doc, version})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DocumentID < result[j].DocumentID })
	return result, nil
}

// Snapshot is a trusted repository read, not a public/native authorization endpoint.
func (l *Ledger) Snapshot(ctx context.Context, s Scope, copyID string) (m Manifest, err error) {
	if !validScope(s) || !id(copyID) {
		return m, ErrInvalid
	}
	if l == nil || l.db == nil {
		return m, ErrUnavailable
	}
	err = l.db.View(ctx, func(tx *sql.Tx) error { var e error; m, e = loadManifest(ctx, tx, s, copyID, 0); return e })
	return
}
