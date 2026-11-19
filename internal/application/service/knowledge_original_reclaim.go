package service

import (
	"context"
	"errors"
	"os"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type knowledgeOriginalRepository interface {
	DeletedKnowledgeOriginal(context.Context, uint64, string, string, string) (*types.Knowledge, error)
	OriginalStorageState(context.Context, *types.Knowledge) (string, error)
}

// ReclaimDeletedAttachment verifies ownership and retries objects for the exact native tombstone.
func (s *knowledgeService) ReclaimDeletedAttachment(ctx context.Context, kbID, id, hash, sealed string) (string, error) {
	tenant, err := writeExecutionTenant(ctx)
	if err != nil {
		return "", err
	}
	repo, ok := s.repo.(knowledgeOriginalRepository)
	if !ok {
		return "", apperrors.NewServiceUnavailableError("original storage verification unavailable")
	}
	row, err := repo.DeletedKnowledgeOriginal(ctx, tenant, kbID, id, hash)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", apperrors.NewConflictError("native deletion is not confirmed")
	}
	lookup, ok := s.repo.(interface {
		AttachmentStorageKnowledgeBase(context.Context, uint64, string) (*types.KnowledgeBase, error)
	})
	if !ok {
		return "", apperrors.NewServiceUnavailableError("attachment storage routing unavailable")
	}
	kb, err := lookup.AttachmentStorageKnowledgeBase(ctx, tenant, kbID)
	if err != nil || kb == nil || kb.TenantID != tenant || kb.ID != kbID {
		return "", apperrors.NewConflictError("attachment storage routing changed")
	}
	ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	if err != nil {
		return "", err
	}
	refs := []string{row.FilePath}
	unresolved := false
	if sealed != "" {
		snapshot, err := extensionbridge.VerifyAttachmentResources(sealed)
		if err != nil || snapshot.Tenant != tenant || snapshot.KB != kbID || snapshot.Document != id || snapshot.Hash != hash {
			return "", apperrors.NewConflictError("attachment resource binding changed")
		}
		refs = append(refs, snapshot.References...)
		unresolved = snapshot.Unresolved
		if s.resourceCatalog != nil {
			current, err := s.resourceCatalog.ListReferencesByOwner(ctx, types.ResourceOwnerKnowledge, row.ID)
			if err != nil {
				return "", err
			}
			refs = append(refs, current...)
		}
	}
	outcome := "removed"
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, registered := types.ParseResourcePath(ref); ref != row.FilePath && !registered {
			return "", apperrors.NewConflictError("legacy derived resource ownership requires review")
		}
		resource := *row
		resource.FilePath = ref
		status, err := s.reclaimKnowledgeOriginal(ctx, s.resolveFileServiceForPath(ctx, kb, ref), &resource)
		if err != nil {
			return "", err
		}
		if status == "shared" {
			outcome = "shared"
		}
	}
	if unresolved {
		return "", apperrors.NewConflictError("legacy derived resource ownership requires review")
	}
	return outcome, nil
}

// reclaimKnowledgeOriginal keeps bytes used by another native document or catalog owner.
func (s *knowledgeService) reclaimKnowledgeOriginal(ctx context.Context, fs interfaces.FileService, row *types.Knowledge) (string, error) {
	if row.FilePath == "" {
		return "removed", nil
	}
	if fs == nil {
		return "", apperrors.NewServiceUnavailableError("document storage unavailable")
	}
	if repo, ok := s.repo.(knowledgeOriginalRepository); ok {
		state, err := repo.OriginalStorageState(ctx, row)
		if err != nil || state != "" {
			return state, err
		}
	}
	_, catalogPath := types.ParseResourcePath(row.FilePath)
	if catalogPath && s.resourceCatalog != nil {
		resource, err := s.resourceCatalog.Resolve(ctx, row.FilePath)
		if err != nil {
			return "", err
		}
		if resource == nil || resource.TenantID != row.TenantID {
			return "", apperrors.NewConflictError("attachment storage owner changed")
		}
		remaining, err := s.resourceCatalog.Release(ctx, row.FilePath, types.ResourceOwnerKnowledge, row.ID)
		if err != nil {
			return "", err
		}
		if remaining > 0 {
			return "shared", nil
		}
	}
	if err := fs.DeleteFile(ctx, row.FilePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if catalogPath && s.resourceCatalog != nil {
			if err := s.resourceCatalog.MarkDeleted(ctx, row.FilePath); err != nil {
				return "", err
			}
		}
	}
	return "removed", nil
}
