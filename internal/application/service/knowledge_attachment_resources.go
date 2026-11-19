package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// attachmentResourceSnapshot collects native references before chunks or owner bindings are removed.
func attachmentResourceSnapshot(ctx context.Context, catalog interfaces.ResourceCatalog, docs []*types.Knowledge, images []interfaces.ChunkImageInfo) (map[string]extensionbridge.AttachmentResourceInventory, error) {
	result := map[string]extensionbridge.AttachmentResourceInventory{}
	byDocument := map[string][]string{}
	for _, image := range images {
		byDocument[image.KnowledgeID] = append(byDocument[image.KnowledgeID], image.ImageInfo)
	}
	for _, doc := range docs {
		if !attachmentReclaimsFile(doc) {
			continue
		}
		inventory := extensionbridge.AttachmentResourceInventory{}
		owned := map[string]bool{doc.FilePath: true}
		if catalog != nil {
			resources, err := catalog.ListReferencesByOwner(ctx, types.ResourceOwnerKnowledge, doc.ID)
			if err != nil {
				return nil, err
			}
			for _, ref := range resources {
				if !owned[ref] {
					inventory.References = append(inventory.References, ref)
					owned[ref] = true
				}
			}
		}
		for _, ref := range collectImageURLs(ctx, byDocument[doc.ID]) {
			if !owned[ref] {
				inventory.Unresolved = true
				break
			}
		}
		result[doc.ID] = inventory
	}
	return result, nil
}

func attachmentReclaimsFile(doc *types.Knowledge) bool {
	return extensionbridge.AttachmentManagedKnowledge(doc) && doc.FileHash != ""
}
