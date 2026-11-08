package attachment

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Receipt identifies the independent native document; source bytes are not returned.
type Receipt struct {
	FileName   string `json:"file_name"`
	DocumentID string `json:"document_id"`
}

// Importer serializes this Host's attachment uploads and limits them to configured KBs.
type Importer struct {
	client   *weknora.Client
	registry *Registry
	allowed  map[string]bool
	mu       sync.Mutex
	db       *store.DB
}

// NewImporter limits uploads to the client's tenant and explicitly configured KBs.
func NewImporter(client *weknora.Client, kbs []string) *Importer {
	allowed := map[string]bool{}
	for _, kb := range kbs {
		allowed[kb] = true
	}
	return &Importer{client: client, registry: DefaultRegistry(), allowed: allowed}
}

// WithSemantics enables durable attachment ownership and reference mapping.
func (i *Importer) WithSemantics(db *store.DB) *Importer {
	i.db = db
	return i
}

// Process imports one level only and records ownership for confirmed uploads.
func (i *Importer) Process(ctx context.Context, tenant uint64, kb, parent, name string, data []byte) ([]Receipt, error) {
	if i == nil || i.client == nil || tenant != i.client.TenantID() || !i.allowed[kb] || !weknora.ValidID(parent) {
		return nil, errors.New("attachment scope denied")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	child, parentTitle, parentFileHash, err := i.client.AttachmentParent(ctx, kb, parent)
	if err != nil {
		return nil, err
	}
	if child {
		return []Receipt{}, nil
	}
	files, err := i.registry.Extract(name, data)
	if err != nil {
		return nil, err
	}
	managedSource := i.db != nil && i.registry.Supports(name, data)
	if managedSource && parentFileHash != fmt.Sprintf("%x", md5.Sum(data)) {
		return nil, errors.New("attachment source revision mismatch")
	}
	result := make([]Receipt, 0, len(files))
	observedIDs := make([]string, 0, len(files))
	bindings := make([]store.AttachmentBinding, 0, len(files))
	for _, file := range files {
		id, importID, err := i.upload(ctx, tenant, kb, parent, parentFileHash, file)
		if err != nil {
			return result, err
		}
		result = append(result, Receipt{file.Name, id})
		observedIDs = append(observedIDs, id)
		if importID != "" {
			bindings = append(bindings, store.AttachmentBinding{ImportID: importID, AttachmentID: id})
		}
	}
	if managedSource {
		isChild, _, currentHash, err := i.client.AttachmentParent(ctx, kb, parent)
		if err != nil || isChild || currentHash != parentFileHash {
			return result, errors.New("attachment parent changed during import")
		}
		if err = i.db.ReconcileAttachmentManifest(ctx, tenant, kb, parent, parentFileHash, bindings, observedIDs); err != nil {
			return result, err
		}
	}
	if i.db != nil && len(files) > 0 {
		semantics := Analyze(name, data, parentTitle, files)
		records := make([]store.AttachmentSemantic, 0, len(semantics))
		for index, semantic := range semantics {
			hash := parentFileHash
			if hash == "" {
				hash = semantic.ParentFileHash
			}
			records = append(records, store.AttachmentSemantic{
				AttachmentID:       result[index].DocumentID,
				TenantID:           tenant,
				KnowledgeBaseID:    kb,
				ParentDocumentID:   parent,
				ParentFileHash:     hash,
				ParentTitle:        semantic.ParentTitle,
				OriginalFilename:   semantic.OriginalFilename,
				DisplayName:        semantic.DisplayName,
				AttachmentSequence: semantic.AttachmentSequence,
				ReferenceTitle:     semantic.ReferenceTitle,
				ReferenceLocation:  semantic.ReferenceLocation,
				ReferenceExcerpt:   semantic.ReferenceExcerpt,
				MappingStatus:      semantic.MappingStatus,
				MappingConfidence:  semantic.MappingConfidence,
				IndexPolicy:        semantic.IndexPolicy,
			})
		}
		if err := i.db.SaveAttachmentSemantics(ctx, records); err != nil {
			// Semantics are an enhancement: never turn a successful upload into
			// a failed import because the mapping table is unavailable.
			log.Printf("attachment semantics: %v", err)
		}
	}
	return result, nil
}

func (i *Importer) upload(ctx context.Context, tenant uint64, kb, parent, parentHash string, file File) (string, string, error) {
	if i.db == nil {
		id, err := i.client.UploadAttachment(ctx, kb, parent, file.Name, file.Data)
		return id, "", err
	}
	importID, err := i.db.PrepareAttachmentImport(ctx, tenant, kb, parent, parentHash, sha256Hex(file.Data), fmt.Sprintf("%x", md5.Sum(file.Data)))
	if err != nil {
		return "", "", err
	}
	id, managed, err := i.client.UploadManagedAttachment(ctx, kb, parent, file.Name, file.Data, importID, func() error {
		return i.db.ClaimAttachmentUpload(ctx, importID)
	})
	if !managed {
		importID = ""
	}
	return id, importID, err
}
