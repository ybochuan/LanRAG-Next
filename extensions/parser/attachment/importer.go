package attachment

import (
	"context"
	"errors"
	"sync"

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
}

// NewImporter limits uploads to the client's tenant and explicitly configured KBs.
func NewImporter(client *weknora.Client, kbs []string) *Importer {
	allowed := map[string]bool{}
	for _, kb := range kbs {
		allowed[kb] = true
	}
	return &Importer{client: client, registry: DefaultRegistry(), allowed: allowed}
}

// Process imports one level only. Parent metadata is a label, not a lineage edge.
func (i *Importer) Process(ctx context.Context, tenant uint64, kb, parent, name string, data []byte) ([]Receipt, error) {
	if i == nil || i.client == nil || tenant != i.client.TenantID() || !i.allowed[kb] || !weknora.ValidID(parent) {
		return nil, errors.New("attachment scope denied")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	child, err := i.client.AttachmentParent(ctx, kb, parent)
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
	result := make([]Receipt, 0, len(files))
	for _, file := range files {
		id, err := i.client.UploadAttachment(ctx, kb, parent, file.Name, file.Data)
		if err != nil {
			return result, err
		}
		result = append(result, Receipt{file.Name, id})
	}
	return result, nil
}
