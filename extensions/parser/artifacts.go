package parser

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// ProxyOption configures the independent proxy before serving requests.
type ProxyOption func(*ScopeProxy) error

// WithArtifactRoot enables document-bound archival. Use data/parser_artifacts
// relative to the service's working directory, or an explicit absolute root.
// Enabling this requires BindDocument before a formal parse job is accepted.
func WithArtifactRoot(root string) ProxyOption {
	return func(p *ScopeProxy) error {
		if strings.TrimSpace(root) == "" || p.artifacts != nil {
			return errors.New("invalid artifact root")
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		p.artifacts = &artifactStore{path: absolute}
		return nil
	}
}

// BindDocument is a trusted in-process assertion from the native upload
// coordinator, not an HTTP parameter or proof of native authorization. Native
// V1 does not transmit doc_id. Never derive it from a filename/hash/job ID.
func (p *ScopeProxy) BindDocument(routeID, sourceFileID, documentID string) error {
	if p == nil || p.artifacts == nil || !safeID(documentID) {
		return errors.New("invalid document binding")
	}
	s := p.scopes[routeID]
	if s == nil {
		return errors.New("invalid document scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.files[sourceFileID] {
		return errors.New("unverified document source")
	}
	if previous := s.documents[sourceFileID]; previous != "" && previous != documentID {
		return errors.New("document binding conflict")
	}
	s.documents[sourceFileID] = documentID
	return nil
}

type artifactRef struct {
	ScopeIdentity
	JobKey        string `json:"-"` // Internal durable formal-job identity, not MinerU schema.
	DocumentID    string `json:"doc_id"`
	SourceFileID  string `json:"source_file_id"`
	MinerUVersion string `json:"mineru_version"`
}
type artifactManifest struct {
	artifactRef
	Format        string `json:"artifact_format"`
	SchemaVersion string `json:"artifact_schema_version"`
	SHA256        string `json:"sha256"`
}
type artifactStore struct {
	mu   sync.Mutex
	path string
	// Set only by in-package fault tests before serving requests; nil uses
	// the platform directory barrier. Never supplied by an HTTP caller.
	syncDirectory func(*os.Root, string) error
}

func (a *artifactStore) syncDir(root *os.Root, name string) error {
	if a.syncDirectory != nil {
		return a.syncDirectory(root, name)
	}
	return syncArtifactDirectory(root, name)
}

const middleJSONLimit = 16 << 20

func extractMiddleZIP(archive []byte) ([]byte, string, error) {
	if len(archive) > proxyResultLimit {
		return nil, "", errors.New("artifact ZIP limit")
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(z.File) > 2048 {
		return nil, "", errors.New("invalid artifact ZIP")
	}
	var middle *zip.File
	for _, f := range z.File {
		name := f.Name
		if name == "" || strings.Contains(name, "\\") || path.IsAbs(name) || path.Clean(name) != strings.TrimSuffix(name, "/") || strings.HasPrefix(name, "../") || f.Mode()&os.ModeSymlink != 0 {
			return nil, "", errors.New("unsafe artifact member")
		}
		base := path.Base(name)
		// MinerU 4.0.1 ParseResult.save writes middle_json.json. The local
		// canonical name is middle.json; content_list is never a substitute.
		if base == "middle_json.json" || base == "middle.json" {
			if middle != nil || f.UncompressedSize64 > middleJSONLimit {
				return nil, "", errors.New("ambiguous or oversized middle JSON")
			}
			middle = f
		}
	}
	if middle == nil {
		return nil, "", errors.New("missing middle JSON")
	}
	r, err := middle.Open()
	if err != nil {
		return nil, "", err
	}
	b, readErr := io.ReadAll(io.LimitReader(r, middleJSONLimit+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil || len(b) > middleJSONLimit {
		return nil, "", errors.New("invalid middle JSON stream")
	}
	var header struct {
		SchemaVersion string          `json:"schema_version"`
		Pages         json.RawMessage `json:"pages"`
	}
	if json.Unmarshal(b, &header) != nil || header.SchemaVersion == "" || len(header.SchemaVersion) > 64 || len(header.Pages) == 0 || header.Pages[0] != '[' {
		return nil, "", errors.New("unrecognized middle JSON envelope")
	}
	return b, header.SchemaVersion, nil
}

func (a *artifactStore) persist(ref artifactRef, archive []byte) error {
	if !safeID(ref.DocumentID) || !safeID(ref.SourceFileID) || ref.TenantID == 0 || !safeID(ref.KnowledgeBaseID) || ref.MinerUVersion == "" || len(ref.MinerUVersion) > 64 {
		return errors.New("incomplete artifact identity")
	}
	b, schema, err := extractMiddleZIP(archive)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	manifest := artifactManifest{artifactRef: ref, Format: "middle_json", SchemaVersion: schema, SHA256: hex.EncodeToString(sum[:])}
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(a.path, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(a.path)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Lstat(ref.DocumentID); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe document artifact target")
		}
		old, err := readArtifact(root, ref.DocumentID+"/manifest.json", 4096)
		if err != nil || !bytes.Equal(old, metadata) {
			return errors.New("artifact ownership/content conflict")
		}
		previous, err := readArtifact(root, ref.DocumentID+"/middle.json", middleJSONLimit)
		if err != nil || !bytes.Equal(previous, b) {
			return errors.New("artifact data conflict")
		}
		// A previous process may have renamed successfully but failed before
		// syncing the parent. Verify original bytes above, then repair barriers.
		if err := a.syncDir(root, ref.DocumentID); err != nil {
			return err
		}
		return a.syncDir(root, ".")
	} else if !os.IsNotExist(err) {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	stage := ".pending-" + hex.EncodeToString(nonce[:])
	if err := root.Mkdir(stage, 0700); err != nil {
		return err
	}
	// Only our exact, newly created staging files/empty directory are removed.
	// os.Root confines every operation; no recursive cleanup or user-file delete.
	defer func() {
		_ = root.Remove(stage + "/middle.json")
		_ = root.Remove(stage + "/manifest.json")
		_ = root.Remove(stage)
	}()
	if err := writeArtifact(root, stage+"/middle.json", b); err != nil {
		return err
	}
	if err := writeArtifact(root, stage+"/manifest.json", metadata); err != nil {
		return err
	}
	if err := a.syncDir(root, stage); err != nil {
		return err
	}
	// Publish both files together. Existing nonempty document directories are
	// never replaced. This is filesystem atomic publication, not a DB transaction.
	if err := root.Rename(stage, ref.DocumentID); err != nil {
		return err
	}
	return a.syncDir(root, ".")
}

func readArtifact(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("unsafe artifact file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("artifact read limit")
	}
	return b, nil
}
func writeArtifact(root *os.Root, name string, b []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(b)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
