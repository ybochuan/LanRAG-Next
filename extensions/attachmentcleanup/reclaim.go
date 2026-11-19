package attachmentcleanup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// WithReclaimer binds the native storage API used only after a verified tombstone.
func (s *Service) WithReclaimer(reclaim func(context.Context, string, string, string) (string, error)) *Service {
	s.reclaim = func(ctx context.Context, kb, id, hash, _ string) (string, error) { return reclaim(ctx, kb, id, hash) }
	return s
}

// WithResourceReclaimer uses the signed native resource manifest for each retry.
func (s *Service) WithResourceReclaimer(reclaim func(context.Context, string, string, string, string) (string, error)) *Service {
	s.reclaim = reclaim
	return s
}

func validateSharedDelete(ctx context.Context, tx *sql.Tx, tenant uint64, kb string, ids []string) error {
	keys, _ := json.Marshal(ids)
	var blocked bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_document_relations r
		JOIN ext_document_version_family a ON a.tenant_id=r.tenant_id AND a.knowledge_base_id=r.knowledge_base_id
		AND a.document_id=r.document_id AND (a.current_version_node_id=r.source_version_id OR `+store.AttachmentPresentationRevisionSQL("r.source_version_id", "a.current_version_node_id")+`)
		JOIN ext_document_version_family t ON t.tenant_id=r.tenant_id AND t.knowledge_base_id=r.target_knowledge_base_id
		AND t.document_id=r.target_document_id AND (t.current_version_node_id=r.target_version_id OR `+store.AttachmentPresentationRevisionSQL("r.target_version_id", "t.current_version_node_id")+`)
		JOIN ext_lifecycle_current c ON c.tenant_id=t.tenant_id AND c.knowledge_base_id=t.knowledge_base_id AND c.document_id=t.document_id
		LEFT JOIN ext_document_annotations p ON p.tenant_id=t.tenant_id AND p.knowledge_base_id=t.knowledge_base_id AND p.document_id=t.document_id
		WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.target_knowledge_base_id=r.knowledge_base_id
		AND r.edge_type='template_for' AND r.status='active' AND c.state='active'
		AND (p.effective_from IS NULL OR julianday(p.effective_from)<=julianday('now'))
		AND (p.effective_until IS NULL OR julianday(p.effective_until)>julianday('now'))
		AND r.document_id IN (SELECT value FROM json_each(?))
		AND r.target_document_id NOT IN (SELECT value FROM json_each(?)))`,
		fmt.Sprint(tenant), kb, string(keys), string(keys)).Scan(&blocked)
	if err == nil && blocked {
		return ErrConflict
	}
	return err
}

type reclaimJob struct {
	kb, id, hash, status, resources string
	indexesCleared                  bool
}

func (s *Service) sweepReclaims(ctx context.Context) error {
	if s.reclaim == nil {
		return nil
	}
	var jobs []reclaimJob
	tenant := fmt.Sprint(s.tenant)
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		// Pre-upgrade jobs did not carry a resource manifest and use the original-file protocol.
		rows, err := tx.QueryContext(ctx, `SELECT knowledge_base_id,document_id,file_hash,status,resources,(indexes_cleared=1 OR resources='') FROM ext_attachment_reclaims
			WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?))
			AND status IN ('deleting','pending') AND next_attempt<=? ORDER BY next_attempt LIMIT 16`,
			tenant, s.scopeJSON, s.now().UnixMilli())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var job reclaimJob
			if err := rows.Scan(&job.kb, &job.id, &job.hash, &job.status, &job.resources, &job.indexesCleared); err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	var failures error
	for _, job := range jobs {
		status := job.status
		code := ""
		var file nativeFile
		err := s.db.ViewNative(ctx, s.nativePath, func(tx *sql.Tx) error {
			var err error
			file, err = readNativeFile(ctx, tx, job.id)
			return err
		})
		if err == nil && file.tenant == s.tenant && file.kb == job.kb && file.hash == job.hash && file.deleted {
			if job.indexesCleared {
				status = "pending"
				outcome, reclaimErr := s.reclaim(ctx, job.kb, job.id, job.hash, job.resources)
				if reclaimErr == nil && (outcome == "removed" || outcome == "shared") {
					status = outcome
				}
				err = reclaimErr
			} else {
				code = "index_cleanup_unconfirmed"
			}
		} else if err == nil && (file.tenant != s.tenant || file.kb != job.kb || file.hash != job.hash) {
			err = ErrConflict
		}
		if err != nil {
			code = "storage_unavailable"
			var upstream *weknora.UpstreamError
			if errors.Is(err, ErrConflict) || errors.As(err, &upstream) && upstream.Status == http.StatusConflict {
				code = "reclaim_requires_review"
			}
		}
		failures = errors.Join(failures, err)
		if err := s.db.Write(ctx, func(tx *sql.Tx) error {
			if status != "deleting" {
				if _, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET removed_at=COALESCE(removed_at,?)
					WHERE tenant_id=? AND knowledge_base_id=? AND attachment_id=? AND attachment_file_hash=?`,
					s.now().UTC().Format(time.RFC3339Nano), tenant, job.kb, job.id, job.hash); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_reclaims SET status=?,next_attempt=?,last_error=?
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND file_hash=?`,
				status, s.now().Add(s.retry).UnixMilli(), code, tenant, job.kb, job.id, job.hash)
			return err
		}); err != nil {
			failures = errors.Join(failures, err)
		}
	}
	return failures
}
