package main

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

type legacyOwnershipPlan struct {
	Version int                           `json:"version"`
	Tenant  uint64                        `json:"tenant_id"`
	KB      string                        `json:"knowledge_base_id"`
	Entries []store.LegacyAttachmentProof `json:"entries"`
	Skipped []legacySkip                  `json:"skipped,omitempty"`
}

type legacySkip struct {
	AttachmentID string `json:"attachment_id"`
	Reason       string `json:"reason"`
}

type legacyFile struct {
	id, name, path, hash, status, enabled, metadata string
}

type legacyMeta struct {
	Attachment string `json:"is_attachment"`
	Parent     string `json:"parent_document_id"`
	Receipt    string `json:"attachment_import_id"`
	Digest     string `json:"attachment_sha256"`
}

type extractedLegacyParent struct {
	file   legacyFile
	digest string
	files  map[string]bool
}

func legacyOwnership(ctx context.Context, extPath, nativePath, filesRoot string, tenant uint64, kb, applyPath string, out io.Writer) error {
	if tenant == 0 || !weknora.ValidID(kb) || nativePath == "" || filesRoot == "" || applyPath != "" && extPath == "" {
		return errors.New("legacy ownership needs -native-db, -files-root, -tenant and -kb; apply also needs -ext-db")
	}
	rootPath, err := filepath.Abs(filesRoot)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	native, err := openNativeReadOnly(nativePath)
	if err != nil {
		return err
	}
	defer native.Close()

	plan := legacyOwnershipPlan{Version: 1, Tenant: tenant, KB: kb, Entries: []store.LegacyAttachmentProof{}}
	var ids []string
	if applyPath == "" {
		ids, err = legacyCandidateIDs(ctx, native, tenant, kb)
	} else {
		plan, err = readLegacyOwnershipPlan(applyPath, tenant, kb)
		for _, entry := range plan.Entries {
			ids = append(ids, entry.AttachmentID)
		}
	}
	if err != nil {
		return err
	}
	cache := make(map[string]extractedLegacyParent)
	verified := make([]store.LegacyAttachmentProof, 0, len(ids))
	for i, id := range ids {
		proof, reason, err := verifyLegacyOwnership(ctx, native, root, tenant, kb, id, cache)
		if err != nil {
			return err
		}
		if applyPath != "" && (reason != "" || proof != plan.Entries[i]) {
			return fmt.Errorf("approved legacy entry %d no longer matches the native files", i+1)
		}
		if reason != "" {
			plan.Skipped = append(plan.Skipped, legacySkip{id, reason})
		} else {
			verified = append(verified, proof)
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if applyPath == "" {
		counts := make(map[[2]string]int)
		for _, entry := range verified {
			counts[[2]string{entry.ParentDocumentID, entry.AttachmentSHA256}]++
		}
		for _, entry := range verified {
			if counts[[2]string{entry.ParentDocumentID, entry.AttachmentSHA256}] != 1 {
				plan.Skipped = append(plan.Skipped, legacySkip{entry.AttachmentID, "ambiguous_same_parent_digest"})
			} else {
				plan.Entries = append(plan.Entries, entry)
			}
		}
		return encoder.Encode(plan)
	}
	return commitLegacyOwnership(ctx, extPath, tenant, kb, verified, encoder)
}

func legacyCandidateIDs(ctx context.Context, native *sql.DB, tenant uint64, kb string) ([]string, error) {
	rows, err := native.QueryContext(ctx, `SELECT id FROM knowledges WHERE tenant_id=? AND knowledge_base_id=?
	AND type='file' AND (deleted_at IS NULL OR deleted_at='')
	AND CASE WHEN json_valid(metadata) THEN metadata->>'is_attachment'='true'
	AND COALESCE(metadata->>'attachment_import_id','')='' ELSE 0 END ORDER BY id`, tenant, kb)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func readLegacyFile(ctx context.Context, native *sql.DB, tenant uint64, kb, id string) (legacyFile, error) {
	var f legacyFile
	err := native.QueryRowContext(ctx, `SELECT id,COALESCE(file_name,''),COALESCE(file_path,''),COALESCE(file_hash,''),
	COALESCE(parse_status,''),COALESCE(enable_status,''),COALESCE(metadata,'{}')
	FROM knowledges WHERE tenant_id=? AND knowledge_base_id=? AND id=? AND type='file'
	AND (deleted_at IS NULL OR deleted_at='')`, tenant, kb, id).
		Scan(&f.id, &f.name, &f.path, &f.hash, &f.status, &f.enabled, &f.metadata)
	return f, err
}

func verifyLegacyOwnership(ctx context.Context, native *sql.DB, root *os.Root, tenant uint64, kb, id string, cache map[string]extractedLegacyParent) (store.LegacyAttachmentProof, string, error) {
	var proof store.LegacyAttachmentProof
	child, err := readLegacyFile(ctx, native, tenant, kb, id)
	if errors.Is(err, sql.ErrNoRows) {
		return proof, "attachment_missing", nil
	}
	if err != nil {
		return proof, "", err
	}
	var meta legacyMeta
	if child.status != "completed" || child.enabled != "enabled" || json.Unmarshal([]byte(child.metadata), &meta) != nil ||
		meta.Attachment != "true" || meta.Receipt != "" || meta.Parent == id || !weknora.ValidID(meta.Parent) || len(meta.Digest) != 64 {
		return proof, "attachment_not_eligible", nil
	}
	parent, err := readLegacyFile(ctx, native, tenant, kb, meta.Parent)
	if errors.Is(err, sql.ErrNoRows) {
		return proof, "parent_missing_or_outside_scope", nil
	}
	if err != nil {
		return proof, "", err
	}
	var parentMeta legacyMeta
	if parent.status != "completed" || parent.enabled != "enabled" || json.Unmarshal([]byte(parent.metadata), &parentMeta) != nil || parentMeta.Attachment == "true" {
		return proof, "parent_not_eligible", nil
	}
	extracted, ok := cache[parent.id]
	if !ok || extracted.file != parent {
		source, err := readLegacyBytes(ctx, native, root, tenant, parent, attachment.MaxSourceBytes)
		if err != nil || fmt.Sprintf("%x", md5.Sum(source)) != parent.hash {
			return proof, "parent_bytes_unverified", nil
		}
		registry := attachment.DefaultRegistry()
		if !registry.Supports(parent.name, source) {
			return proof, "parent_format_unsupported", nil
		}
		files, err := registry.Extract(parent.name, source)
		if err != nil {
			return proof, "parent_extraction_failed", nil
		}
		extracted = extractedLegacyParent{parent, fmt.Sprintf("%x", sha256.Sum256(source)), make(map[string]bool, len(files))}
		for _, file := range files {
			extracted.files[fmt.Sprintf("%x", sha256.Sum256(file.Data))] = true
		}
		cache[parent.id] = extracted
	}
	childBytes, err := readLegacyBytes(ctx, native, root, tenant, child, attachment.MaxFileBytes-1)
	if err != nil || fmt.Sprintf("%x", md5.Sum(childBytes)) != child.hash ||
		fmt.Sprintf("%x", sha256.Sum256(childBytes)) != meta.Digest || !extracted.files[meta.Digest] {
		return proof, "attachment_bytes_do_not_match_extraction", nil
	}
	return store.LegacyAttachmentProof{
		ParentDocumentID: parent.id, AttachmentID: child.id, ParentFileHash: parent.hash,
		ParentSHA256: extracted.digest, AttachmentSHA256: meta.Digest, AttachmentFileHash: child.hash,
	}, "", nil
}

var resourceHandle = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// legacyLocalPath resolves the native catalog entry only for a local backend
// that uses the operator's supplied files root without a path prefix.
func legacyLocalPath(ctx context.Context, native *sql.DB, tenant uint64, doc legacyFile) (string, error) {
	handle, isResource := strings.CutPrefix(doc.path, "resource://")
	if !isResource {
		return doc.path, nil
	}
	if !resourceHandle.MatchString(handle) {
		return "", errors.New("invalid native resource handle")
	}
	var path, backend string
	err := native.QueryRowContext(ctx, `
		SELECT r.physical_path,b.id FROM resources r
		JOIN storage_backends b ON b.id=r.storage_backend_id AND b.tenant_id=r.tenant_id
		WHERE r.handle=? AND r.tenant_id=? AND r.provider='local' AND r.state='active' AND r.deleted_at IS NULL
		AND b.provider='local' AND b.status='active' AND b.deleted_at IS NULL
		AND CASE WHEN json_valid(b.config) THEN COALESCE(b.config->>'path_prefix','')='' ELSE 0 END
		AND EXISTS (
			SELECT 1 FROM resource_bindings rb WHERE rb.resource_id=r.id AND rb.tenant_id=r.tenant_id
			AND rb.owner_type='knowledge' AND rb.owner_id=? AND rb.relation='source_file'
		)`, handle, tenant, doc.id).Scan(&path, &backend)
	if err != nil {
		return "", err
	}
	path, ok := strings.CutPrefix(path, "storage://"+backend+"/")
	if !weknora.ValidID(backend) || !ok || !strings.HasPrefix(path, "local://") {
		return "", errors.New("resource path does not match the verified local backend")
	}
	return path, nil
}

func readLegacyBytes(ctx context.Context, native *sql.DB, root *os.Root, tenant uint64, doc legacyFile, limit int) ([]byte, error) {
	path, err := legacyLocalPath(ctx, native, tenant, doc)
	if err != nil {
		return nil, err
	}
	rel, local := strings.CutPrefix(path, "local://")
	if !local {
		if !filepath.IsAbs(path) {
			return nil, errors.New("non-local source")
		}
		rel, err = filepath.Rel(root.Name(), path)
		if err != nil {
			return nil, err
		}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[0] != strconv.FormatUint(tenant, 10) || parts[1] != doc.id || !weknora.ValidID(parts[2]) {
		return nil, errors.New("source path outside the native document directory")
	}
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unverified source storage entry")
		}
	}
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > int64(limit) {
		return nil, errors.New("source is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) == 0 || len(data) > limit {
		return nil, errors.New("source read failed")
	}
	return data, nil
}

func readLegacyOwnershipPlan(path string, tenant uint64, kb string) (legacyOwnershipPlan, error) {
	var plan legacyOwnershipPlan
	file, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	defer file.Close()
	const limit = 4 << 20
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return plan, errors.New("invalid ownership plan file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF ||
		plan.Version != 1 || plan.Tenant != tenant || plan.KB != kb || len(plan.Entries) == 0 {
		return plan, errors.New("invalid or out-of-scope legacy ownership plan")
	}
	seen := make(map[string]bool)
	for _, p := range plan.Entries {
		if !weknora.ValidID(p.AttachmentID) || seen[p.AttachmentID] {
			return plan, errors.New("ownership plan contains an invalid or duplicate attachment ID")
		}
		seen[p.AttachmentID] = true
	}
	return plan, nil
}

func commitLegacyOwnership(ctx context.Context, path string, tenant uint64, kb string, entries []store.LegacyAttachmentProof, encoder *json.Encoder) error {
	reader, err := store.OpenReadOnly(ctx, path)
	if err != nil {
		return err
	}
	var version int
	err = reader.QueryRowContext(ctx, "SELECT MAX(version) FROM ext_schema_migrations").Scan(&version)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if version < 18 || version > store.LatestSchemaVersion {
		return errors.New("apply requires an existing supported extension database with attachment revision schema")
	}
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.AdoptLegacyAttachments(ctx, tenant, kb, entries); err != nil {
		return err
	}
	return encoder.Encode(map[string]int{"adopted": len(entries)})
}
