// Package attachmentcleanup coordinates native deletion of verified attachments.
package attachmentcleanup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

var (
	// ErrScope means a request cannot be bound to the configured worker scope.
	ErrScope = errors.New("attachment deletion scope denied")
	// ErrConflict means an upload receipt or current attachment no longer matches.
	ErrConflict = errors.New("attachment deletion identity unresolved")
)

// Target is a native file snapshot taken after authorization and before deletion.
type Target struct {
	DocumentID       string `json:"document_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id"`
	FileHash         string `json:"file_hash"`
	ParentDocumentID string `json:"parent_document_id,omitempty"`
	ImportID         string `json:"import_id,omitempty"`
	IsAttachment     bool   `json:"is_attachment,omitempty"`
	Title            string `json:"title,omitempty"`
}

// Service persists intent in the extension DB and only reads the native DB.
type Service struct {
	db         *store.DB
	nativePath string
	tenant     uint64
	kbs        map[string]bool
	scopeJSON  string
	delete     func(context.Context, string, string, string) error
	reclaim    func(context.Context, string, string, string) (string, error)
	retry      time.Duration
	now        func() time.Time
}

// New binds the worker to the same SQLite DB and scope as the native Core.
func New(db *store.DB, nativePath string, tenant uint64, kbs []string, deleteChild func(context.Context, string, string, string) error, retry time.Duration) *Service {
	allowed := make(map[string]bool, len(kbs))
	for _, kb := range kbs {
		allowed[kb] = true
	}
	encoded, _ := json.Marshal(kbs)
	return &Service{db: db, nativePath: nativePath, tenant: tenant, kbs: allowed, scopeJSON: string(encoded), delete: deleteChild, retry: retry, now: time.Now}
}

// Prepare atomically freezes attachment uploads for the native deletion batch.
// A child scheduled by this worker must still match its receipt when the
// native queue actually reaches the authorized deletion checkpoint.
func (s *Service) Prepare(ctx context.Context, tenant uint64, targets []Target) error {
	return s.prepare(ctx, tenant, targets, false)
}

func (s *Service) Validate(ctx context.Context, tenant uint64, targets []Target) error {
	return s.prepare(ctx, tenant, targets, true)
}

func (s *Service) prepare(ctx context.Context, tenant uint64, targets []Target, checkOnly bool) error {
	if tenant == 0 || tenant != s.tenant || len(targets) == 0 {
		return ErrScope
	}
	for _, target := range targets {
		if !s.kbs[target.KnowledgeBaseID] || !weknora.ValidID(target.DocumentID) || len(target.DocumentID) > 128 {
			return ErrScope
		}
	}
	byKB := map[string][]string{}
	for _, target := range targets {
		byKB[target.KnowledgeBaseID] = append(byKB[target.KnowledgeBaseID], target.DocumentID)
	}
	check := func(tx *sql.Tx) error {
		for kb, ids := range byKB {
			if err := validateSharedDelete(ctx, tx, tenant, kb, ids); err != nil {
				return err
			}
		}
		return nil
	}
	if checkOnly {
		return s.db.View(ctx, check)
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := check(tx); err != nil {
			return err
		}
		owner := fmt.Sprint(tenant)
		stamp := s.now().UTC().Format(time.RFC3339Nano)
		for _, target := range targets {
			var unsafe bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM ext_attachment_links WHERE tenant_id=? AND (
					(knowledge_base_id=? AND parent_document_id=? AND submitted=1 AND attachment_id IS NULL)
					OR (attachment_id=? AND cleanup_started=1 AND (
						knowledge_base_id<>? OR attachment_file_hash<>? OR parent_document_id<>?
						OR (evidence_kind='upload_receipt' AND import_id<>?)
						OR (evidence_kind='legacy_reextracted' AND ?<>'') OR ?=0
					))
				))`, owner, target.KnowledgeBaseID, target.DocumentID,
				target.DocumentID, target.KnowledgeBaseID, target.FileHash, target.ParentDocumentID, target.ImportID, target.ImportID, target.IsAttachment).Scan(&unsafe)
			if err != nil {
				return err
			}
			if unsafe {
				return ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ext_attachment_delete_intents VALUES(?,?,?,?)
				ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO NOTHING`, owner, target.KnowledgeBaseID, target.DocumentID, stamp); err != nil {
				return err
			}
			if s.reclaim != nil && target.FileHash != "" {
				if _, err = tx.ExecContext(ctx, `INSERT INTO ext_attachment_reclaims(tenant_id,knowledge_base_id,document_id,parent_document_id,file_hash,title)
					VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO NOTHING`,
					owner, target.KnowledgeBaseID, target.DocumentID, target.ParentDocumentID, target.FileHash, target.Title); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type job struct {
	receipt, kb, parent, child, childHash string
	evidence, childSHA                    string
	next                                  int64
}

func (s *Service) due(ctx context.Context) ([]job, error) {
	var jobs []job
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT a.import_id,a.knowledge_base_id,a.parent_document_id,a.attachment_id,a.attachment_file_hash,
			a.evidence_kind,a.attachment_sha256,a.cleanup_next_at
			FROM ext_attachment_links a
			JOIN ext_attachment_delete_intents d ON d.tenant_id=a.tenant_id AND d.knowledge_base_id=a.knowledge_base_id AND d.document_id=a.parent_document_id
			WHERE a.tenant_id=? AND a.knowledge_base_id IN (SELECT value FROM json_each(?))
			AND a.attachment_id IS NOT NULL AND a.removed_at IS NULL AND a.detached_at IS NULL AND a.cleanup_next_at<=?
			ORDER BY a.cleanup_next_at,a.import_id LIMIT 32`, fmt.Sprint(s.tenant), s.scopeJSON, s.now().UnixMilli())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j job
			if err := rows.Scan(&j.receipt, &j.kb, &j.parent, &j.child, &j.childHash, &j.evidence, &j.childSHA, &j.next); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, err
}

type nativeFile struct {
	tenant           uint64
	kb, hash, status string
	deleted          bool
	metadata         []byte
}

func readNativeFile(ctx context.Context, tx *sql.Tx, id string) (nativeFile, error) {
	var f nativeFile
	err := tx.QueryRowContext(ctx, `SELECT tenant_id,knowledge_base_id,COALESCE(file_hash,''),COALESCE(parse_status,''),
		deleted_at IS NOT NULL AND deleted_at<>'',metadata FROM weknora.knowledges WHERE id=?`, id).
		Scan(&f.tenant, &f.kb, &f.hash, &f.status, &f.deleted, &f.metadata)
	return f, err
}

// Sweep reconciles a bounded batch. Native async acceptance does not complete
// cleanup; only a verified native tombstone does.
func (s *Service) Sweep(ctx context.Context) error {
	jobs, err := s.due(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = errors.Join(err, s.process(ctx, j))
	}
	return errors.Join(err, s.sweepReclaims(ctx))
}

func (s *Service) process(ctx context.Context, j job) error {
	var parent, child nativeFile
	err := s.db.ViewNative(ctx, s.nativePath, func(tx *sql.Tx) error {
		var err error
		parent, err = readNativeFile(ctx, tx, j.parent)
		if err != nil {
			return err
		}
		if parent.tenant != s.tenant || parent.kb != j.kb {
			return ErrConflict
		}
		if !parent.deleted {
			return nil
		}
		child, err = readNativeFile(ctx, tx, j.child)
		if err == nil && (child.tenant != s.tenant || child.kb != j.kb) {
			return ErrConflict
		}
		return err
	})
	if err != nil {
		return errors.Join(err, s.deferJob(ctx, j, "native_deletion_unverified"))
	}
	if !parent.deleted || !child.deleted && (child.status == "deleting" || child.status == "moving") {
		return s.deferJob(ctx, j, "")
	}
	if child.deleted {
		return s.db.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET removed_at=?, cleanup_error=''
				WHERE import_id=? AND removed_at IS NULL`, s.now().UTC().Format(time.RFC3339Nano), j.receipt)
			return err
		})
	}
	var meta struct {
		Attachment string `json:"is_attachment"`
		Parent     string `json:"parent_document_id"`
		Receipt    string `json:"attachment_import_id"`
		SHA256     string `json:"attachment_sha256"`
	}
	if child.hash != j.childHash || json.Unmarshal(child.metadata, &meta) != nil ||
		meta.Attachment != "true" || meta.Parent != j.parent {
		return errors.Join(ErrConflict, s.deferJob(ctx, j, "attachment_identity_conflict"))
	}
	receiptMatches := j.evidence == store.AttachmentUploadEvidence && meta.Receipt == j.receipt ||
		j.evidence == store.AttachmentLegacyEvidence && meta.Receipt == "" && meta.SHA256 == j.childSHA
	if !receiptMatches {
		return errors.Join(ErrConflict, s.deferJob(ctx, j, "attachment_identity_conflict"))
	}
	claimed := false
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links
			SET cleanup_started=1, cleanup_next_at=?, cleanup_error=''
			WHERE import_id=? AND removed_at IS NULL AND cleanup_next_at=?`, s.now().Add(s.retry).UnixMilli(), j.receipt, j.next)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		claimed = n == 1
		return err
	}); err != nil || !claimed {
		return err
	}
	if err := s.delete(ctx, j.kb, j.parent, j.child); err != nil {
		writeErr := s.db.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET cleanup_error='native_delete_unconfirmed'
				WHERE import_id=? AND removed_at IS NULL`, j.receipt)
			return err
		})
		return errors.Join(err, writeErr)
	}
	return nil
}

func (s *Service) deferJob(ctx context.Context, j job, code string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_attachment_links SET cleanup_next_at=?,cleanup_error=?
			WHERE import_id=? AND cleanup_next_at=? AND removed_at IS NULL`, s.now().Add(s.retry).UnixMilli(), code, j.receipt, j.next)
		return err
	})
}

// Run periodically resumes cleanup, including intents persisted before restart.
func (s *Service) Run(ctx context.Context, interval time.Duration, report func(error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return ctx.Err()
}
