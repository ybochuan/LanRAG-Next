package service

import (
	"context"
	"errors"
	"os"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type knowledgeOriginalRepository interface {
	DeletedKnowledgeOriginal(context.Context, uint64, string, string, string) (*types.Knowledge, error)
	OriginalStorageState(context.Context, *types.Knowledge) (string, error)
}

// ReclaimDeletedAttachment retries only the source object of an exact native tombstone.
func (s *knowledgeService) ReclaimDeletedAttachment(ctx context.Context, kbID, id, hash string) (string, error) {
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
	kb, err := knowledgeWriteKB(ctx, s.kbService, row)
	if err != nil {
		return "", err
	}
	if _, err = requireKBWrite(ctx, kb); err != nil {
		return "", err
	}
	ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	if err != nil {
		return "", err
	}
	return s.reclaimKnowledgeOriginal(ctx, s.resolveFileService(ctx, kb), row)
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
