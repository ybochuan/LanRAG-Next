package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func auditOwnership(ctx context.Context, extPath, nativePath string, tenant uint64, kb string, out io.Writer) error {
	if extPath == "" || nativePath == "" || tenant == 0 {
		return errors.New("-ext-db, -native-db and a positive -tenant are required")
	}
	ext, err := store.OpenReadOnly(ctx, extPath)
	if err != nil {
		return err
	}
	defer ext.Close()
	tx, err := ext.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM ext_schema_migrations").Scan(&version); err != nil {
		return err
	}
	native, err := openNativeReadOnly(nativePath)
	if err != nil {
		return err
	}
	defer native.Close()
	rows, err := native.QueryContext(ctx, `WITH candidates AS (
		SELECT id,tenant_id,knowledge_base_id,COALESCE(file_hash,'') file_hash,
			COALESCE(metadata->>'parent_document_id','') parent_id, COALESCE(metadata->>'attachment_import_id','') receipt,
			COALESCE(metadata->>'attachment_sha256','') sha
		FROM knowledges WHERE tenant_id=? AND (deleted_at IS NULL OR deleted_at='')
		AND (?='' OR knowledge_base_id=?)
		AND CASE WHEN json_valid(metadata) THEN metadata->>'is_attachment'='true' ELSE 0 END
	)
	SELECT c.id,c.knowledge_base_id,c.file_hash,c.parent_id,c.receipt,c.sha,
		COALESCE(p.knowledge_base_id,''),COALESCE(p.file_hash,''),
		COALESCE(p.parse_status,'')='deleting'
	FROM candidates c LEFT JOIN knowledges p ON p.id=c.parent_id AND p.tenant_id=c.tenant_id
		AND (p.deleted_at IS NULL OR p.deleted_at='')`, tenant, kb, kb)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := map[string]int{"verified": 0, "legacy": 0, "parent_unavailable": 0, "scope_conflict": 0, "receipt_or_revision_mismatch": 0}
	for rows.Next() {
		var id, childKB, childHash, parentID, receipt, sha, parentKB, parentHash string
		var deleting bool
		if err := rows.Scan(&id, &childKB, &childHash, &parentID, &receipt, &sha, &parentKB, &parentHash, &deleting); err != nil {
			return err
		}
		switch {
		case parentKB == "" || deleting:
			counts["parent_unavailable"]++
		case id == parentID || parentKB != childKB:
			counts["scope_conflict"]++
		default:
			owners := map[string]store.AttachmentOwnership{}
			if version >= 18 {
				owners, err = store.AttachmentOwnershipsTx(ctx, tx, tenant, []string{id})
			} else if version >= 15 {
				owners, err = store.AttachmentOwnershipsBeforeRevisionsTx(ctx, tx, tenant, []string{id})
			}
			if version >= 15 {
				if err != nil {
					return err
				}
			}
			owner, linked := owners[id]
			proof := owner.EvidenceKind == store.AttachmentUploadEvidence && owner.ImportID == receipt ||
				owner.EvidenceKind == store.AttachmentLegacyEvidence && receipt == "" && owner.AttachmentSHA256 == sha
			switch {
			case !linked && receipt == "":
				counts["legacy"]++
			case linked && proof && owner.KnowledgeBaseID == childKB &&
				owner.ParentDocumentID == parentID && owner.ParentFileHash == parentHash && owner.AttachmentFileHash == childHash:
				counts["verified"]++
			default:
				counts["receipt_or_revision_mismatch"]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(counts)
}

func openNativeReadOnly(nativePath string) (*sql.DB, error) {
	abs, err := filepath.Abs(nativePath)
	if err != nil {
		return nil, err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&_pragma=query_only(ON)"}
	return sql.Open("sqlite", u.String())
}
