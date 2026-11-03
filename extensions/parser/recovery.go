package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Parser state is versioned independently of MinerU's result schema. Credentials
// stay in operator configuration; snapshots contain document capabilities only.
type documentSnapshot struct {
	Version         int
	Uploads         map[string]uploadSnapshot
	Files           map[string]bool
	Hashes          map[string]string
	PDFs            map[string]PDFPreflight
	Documents       map[string]string
	JobDocuments    map[string]artifactRef
	OutputDocuments map[string]artifactRef
	OutputJobKeys   map[string]string
	JobKeys         map[string]string
	Jobs            map[string]bool
	Requests        map[string]receiptSnapshot
}
type uploadSnapshot struct {
	Size                                    int64
	Hash, BackendID, BackendFile, UploadURL string
	Verified, DeclaredPDF                   bool
	Preflight                               *PDFPreflight
	Filename                                string
}
type receiptSnapshot struct {
	Status int
	Body   []byte
}
type parserJournal struct {
	db       *store.DB
	identity string
	owner    nativeIdentity
	root     string
}

// NewPersistentNativeHandler is the production variant. The caller owns the
// extension-only DB and closes it after draining HTTP and closing the handler.
func NewPersistentNativeHandler(cfg NativeConfig, db *store.DB) (*NativeHandler, error) {
	if !db.FullSync() {
		return nil, errors.New("parser requires a FULL synchronous extension database")
	}
	h, err := NewNativeHandler(cfg)
	if err != nil {
		return nil, err
	}
	h.db = db
	return h, nil
}

func newJournal(db *store.DB, id nativeIdentity, cfg NativeConfig) *parserJournal {
	root, _ := filepath.Abs(cfg.ArtifactRoot)
	b, _ := json.Marshal([]any{1, id, strings.TrimSuffix(cfg.UpstreamURL, "/"), root})
	sum := sha256.Sum256(b)
	return &parserJournal{db: db, identity: hex.EncodeToString(sum[:]), owner: id, root: root}
}

func snapshot(s *scopeState) ([]byte, documentSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := documentSnapshot{Version: 1, Uploads: map[string]uploadSnapshot{}, Files: s.files, Hashes: s.hashes, PDFs: s.pdfs, Documents: s.documents, JobDocuments: s.jobDocuments, OutputDocuments: s.outputDocuments, OutputJobKeys: map[string]string{}, JobKeys: s.jobKeys, Jobs: s.jobs, Requests: map[string]receiptSnapshot{}}
	for k, ref := range s.outputDocuments {
		d.OutputJobKeys[k] = ref.JobKey
	}
	for k, u := range s.uploads {
		d.Uploads[k] = uploadSnapshot{u.size, u.hash, u.backendID, u.backendFile, u.uploadURL, u.verified, u.declaredPDF, u.preflight, u.filename}
	}
	for k, r := range s.requests {
		d.Requests[hex.EncodeToString(k[:])] = receiptSnapshot{r.status, r.body}
	}
	b, err := json.Marshal(d)
	return b, d, err
}

func (j *parserJournal) restore(ctx context.Context, s *scopeState) error {
	var b []byte
	err := j.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT snapshot FROM ext_parser_documents WHERE identity_key=?", j.identity).Scan(&b)
	})
	if errors.Is(err, sql.ErrNoRows) {
		// Upgrading pre-journal installations (or losing the state DB) cannot
		// reconstruct job ownership from middle.json. Refuse before any new
		// parse side effect; never reinterpret an existing archive as a new doc.
		root, openErr := os.OpenRoot(j.root)
		if os.IsNotExist(openErr) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		defer root.Close()
		_, archiveErr := root.Lstat(j.owner.DocID)
		if archiveErr == nil {
			return errors.New("existing archive requires verified job history")
		}
		if !os.IsNotExist(archiveErr) {
			return archiveErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	var d documentSnapshot
	if json.Unmarshal(b, &d) != nil || d.Version != 1 || d.Uploads == nil || d.Files == nil || d.Hashes == nil || d.PDFs == nil || d.Documents == nil || d.JobDocuments == nil || d.OutputDocuments == nil || d.JobKeys == nil || d.Jobs == nil || d.Requests == nil {
		return errors.New("invalid parser snapshot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files, s.hashes, s.pdfs, s.documents, s.jobDocuments, s.outputDocuments, s.jobKeys, s.jobs = d.Files, d.Hashes, d.PDFs, d.Documents, d.JobDocuments, d.OutputDocuments, d.JobKeys, d.Jobs
	for k, u := range d.Uploads {
		s.uploads[k] = &sourceUpload{size: u.Size, hash: u.Hash, backendID: u.BackendID, backendFile: u.BackendFile, uploadURL: u.UploadURL, verified: u.Verified, declaredPDF: u.DeclaredPDF, preflight: u.Preflight, filename: u.Filename}
	}
	for k, r := range d.Requests {
		raw, err := hex.DecodeString(k)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid parser receipt")
		}
		var key [32]byte
		copy(key[:], raw)
		s.requests[key] = &jobReceipt{status: r.Status, body: r.Body}
	}
	for job, ref := range s.jobDocuments {
		ref.JobKey = s.jobKeys[job]
		s.jobDocuments[job] = ref
	}
	for file, ref := range s.outputDocuments {
		ref.JobKey = d.OutputJobKeys[file]
		s.outputDocuments[file] = ref
	}
	return nil
}

func (j *parserJournal) putDocument(ctx context.Context, tx *sql.Tx, b []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_parser_documents VALUES(?,?,?,?,?,?) ON CONFLICT(identity_key) DO UPDATE SET snapshot=excluded.snapshot,updated_at=excluded.updated_at`, j.identity, j.owner.TenantID, j.owner.KnowledgeBaseID, j.owner.DocID, b, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

var errIntentExists = errors.New("parser intent already exists")

// claim commits an unknown intent BEFORE the first formal remote POST. Its
// uniqueness is enforced by SQLite as well as the document request mutex.
func (j *parserJournal) claim(ctx context.Context, s *scopeState, key [32]byte) error {
	b, _, err := snapshot(s)
	if err != nil {
		return err
	}
	return j.db.Write(ctx, func(tx *sql.Tx) error {
		if err := j.putDocument(ctx, tx, b); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO ext_parser_jobs(identity_key,job_key,status) VALUES(?,?,0)", j.identity, hex.EncodeToString(key[:]))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errIntentExists
		}
		return nil
	})
}

func (j *parserJournal) save(ctx context.Context, s *scopeState) error {
	b, d, err := snapshot(s)
	if err != nil {
		return err
	}
	return j.db.Write(ctx, func(tx *sql.Tx) error {
		if err := j.putDocument(ctx, tx, b); err != nil {
			return err
		}
		for key, r := range d.Requests {
			if r.Status == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE ext_parser_jobs SET status=?,receipt=? WHERE identity_key=? AND job_key=? AND status=0", r.Status, r.Body, j.identity, key); err != nil {
				return err
			}
		}
		return nil
	})
}

// register follows verified filesystem publication. If this transaction fails,
// retry checks the same files and repairs registration before releasing the ZIP.
func (j *parserJournal) register(ctx context.Context, ref artifactRef, archive []byte) error {
	b, schema, err := extractMiddleZIP(archive)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	if ref.JobKey == "" {
		return errors.New("artifact formal job required")
	}
	return j.db.Write(ctx, func(tx *sql.Tx) error {
		var status int
		if err := tx.QueryRowContext(ctx, "SELECT status FROM ext_parser_jobs WHERE identity_key=? AND job_key=?", j.identity, ref.JobKey).Scan(&status); err != nil {
			return err
		}
		if status == 0 {
			return errors.New("artifact job outcome unknown")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_parser_artifacts VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(identity_key) DO NOTHING`, j.identity, ref.JobKey, ref.TenantID, ref.KnowledgeBaseID, ref.DocumentID, ref.SourceFileID, "middle_json", schema, ref.MinerUVersion, hash, b, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		var storedHash, storedKey string
		if err := tx.QueryRowContext(ctx, "SELECT sha256,job_key FROM ext_parser_artifacts WHERE identity_key=?", j.identity).Scan(&storedHash, &storedKey); err != nil {
			return err
		}
		if storedHash != hash || storedKey != ref.JobKey {
			return errors.New("artifact registration conflict")
		}
		return nil
	})
}
