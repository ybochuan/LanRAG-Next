package attachment

import (
	"context"
	"crypto/md5"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// PreviewFile describes a candidate without storing or returning the file bytes.
type PreviewFile struct {
	Fingerprint string `json:"fingerprint"`
	FileName    string `json:"file_name"`
	DisplayName string `json:"display_name"`
	Mapping     string `json:"mapping_status"`
	State       string `json:"state"`
}

// ImportOutcome identifies the result of one selected attachment upload.
type ImportOutcome struct {
	Fingerprint string `json:"fingerprint"`
	FileName    string `json:"file_name"`
	DocumentID  string `json:"document_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ParentReference is an evidence choice, scoped to a single checked source revision.
type ParentReference struct {
	ID string `json:"id"`
	store.AttachmentReference
}

// Source reads the parent with the native file service and verifies the current revision.
func (i *Importer) Source(ctx context.Context, tenant uint64, kb, parent, expectedHash string) (string, string, []byte, error) {
	if i == nil || i.client == nil || i.db == nil || tenant != i.client.TenantID() || !i.allowed[kb] {
		return "", "", nil, store.ErrAttachmentConflict
	}
	docs, err := i.client.AttachmentDocuments(ctx, []string{kb}, []string{parent})
	if err != nil {
		return "", "", nil, err
	}
	if len(docs) != 1 || docs[0].IsAttachment || docs[0].ParseStatus == "deleting" || docs[0].FileHash == "" ||
		(expectedHash != "" && docs[0].FileHash != expectedHash) {
		return "", "", nil, store.ErrAttachmentConflict
	}
	d := docs[0]
	data, err := i.client.OriginalBytes(ctx, parent, MaxSourceBytes)
	if err != nil {
		return "", "", nil, err
	}
	if fmt.Sprintf("%x", md5.Sum(data)) != d.FileHash {
		return "", "", nil, store.ErrAttachmentConflict
	}
	return d.FileName, d.FileHash, data, nil
}

// ReferenceOptions reuses the same parser that generated the automatic suggestions.
func ReferenceOptions(name string, data []byte) []ParentReference {
	options := []ParentReference{}
	for index, ref := range parentReferences(name, data) {
		if len(options) >= 128 {
			break
		}
		options = append(options, ParentReference{
			ID: strconv.Itoa(index + 1),
			AttachmentReference: store.AttachmentReference{
				Title: ref.Title, Location: ref.Location, Excerpt: ref.Excerpt, Sequence: ref.Ordinal,
			},
		})
	}
	return options
}

// Preview reads a supported source and returns its independently selectable files.
func (i *Importer) Preview(ctx context.Context, tenant uint64, kb, parent, version string) ([]PreviewFile, []ParentReference, []ExtractionIssue, error) {
	name, current, data, err := i.Source(ctx, tenant, kb, parent, version)
	if err != nil {
		return nil, nil, nil, err
	}
	manifest, err := i.registry.extract(name, data)
	if err != nil {
		return nil, nil, nil, err
	}
	states, err := i.db.AttachmentImportState(ctx, tenant, kb, parent, current)
	if err != nil {
		return nil, nil, nil, err
	}
	items := []PreviewFile{}
	files := manifest.files
	seen := map[string]bool{}
	semantics := Analyze(name, data, "", files)
	for index, file := range files {
		fingerprint := sha256Hex(file.Data)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		state := states[fingerprint]
		if state == "" {
			state = "new"
		}
		items = append(items, PreviewFile{fingerprint, file.Name, semantics[index].DisplayName, semantics[index].MappingStatus, state})
	}
	return items, ReferenceOptions(name, data), manifest.issues, nil
}

// ImportSelected reconciles the given preview selection without retiring other files.
func (i *Importer) ImportSelected(ctx context.Context, tenant uint64, kb, parent, version string, fingerprints []string) ([]ImportOutcome, error) {
	if version == "" || len(fingerprints) == 0 || len(fingerprints) > MaxFiles {
		return nil, store.ErrAttachmentConflict
	}
	name, _, data, err := i.Source(ctx, tenant, kb, parent, version)
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, fingerprint := range fingerprints {
		if len(fingerprint) != 64 || selected[fingerprint] {
			return nil, store.ErrAttachmentConflict
		}
		selected[fingerprint] = true
	}
	var outcomes []ImportOutcome
	_, err = i.process(ctx, tenant, kb, parent, name, data, version, selected, &outcomes)
	return outcomes, err
}

// Upload verifies the explicit upload against the same native receipt protocol.
func (i *Importer) Upload(ctx context.Context, tenant uint64, kb, parent, version, name string, data []byte) (Receipt, error) {
	var out Receipt
	if i == nil || i.client == nil || i.db == nil || tenant != i.client.TenantID() || !i.allowed[kb] || version == "" ||
		path.Base(name) != name || strings.Contains(name, "\\") || !Allowed(name, data) {
		return out, store.ErrAttachmentConflict
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	child, _, current, err := i.client.AttachmentParent(ctx, kb, parent)
	if err != nil {
		return out, err
	}
	if child || current != version {
		return out, store.ErrAttachmentConflict
	}
	states, err := i.db.AttachmentImportState(ctx, tenant, kb, parent, current)
	if err != nil {
		return out, err
	}
	if states[sha256Hex(data)] == "removed" {
		return out, store.ErrAttachmentConflict
	}
	id, importID, err := i.upload(ctx, tenant, kb, parent, version, File{Name: name, Data: data})
	if err != nil {
		return out, err
	}
	if importID == "" {
		return out, store.ErrAttachmentConflict
	}
	child, _, current, err = i.client.AttachmentParent(ctx, kb, parent)
	if err != nil {
		return out, err
	}
	if child || current != version {
		return out, store.ErrAttachmentConflict
	}
	if err = i.db.BindManualAttachment(ctx, tenant, kb, parent, version, store.AttachmentBinding{ImportID: importID, AttachmentID: id}); err != nil {
		return out, err
	}
	return Receipt{FileName: name, DocumentID: id}, nil
}
