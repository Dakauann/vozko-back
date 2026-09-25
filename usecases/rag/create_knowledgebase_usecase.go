package rag_usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"vozko/domain/rag"
	wd "vozko/domain/workspace/workspace_department"
)

type createKnowledgeBaseUseCase struct {
	repo        rag.KnowledgeBaseRepository
	departments wd.CreationDepartmentResolver
}

func NewCreateKnowledgeBaseUseCase(repo rag.KnowledgeBaseRepository, departments wd.CreationDepartmentResolver) rag.CreateKnowledgeBaseUseCase {
	return &createKnowledgeBaseUseCase{repo: repo, departments: departments}
}

func (uc *createKnowledgeBaseUseCase) Execute(ctx context.Context, input rag.CreateKnowledgeBaseInput) (*rag.KnowledgeBase, error) {
	if uc.departments == nil {
		return nil, wd.ErrDepartmentAccessDenied
	}
	departmentID, err := uc.departments.Resolve(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}

	count, err := uc.repo.CountByWorkspace(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if count >= rag.MaxKnowledgeBasesPerWorkspace {
		return nil, rag.ErrMaxKnowledgeBasesReached
	}

	config := input.Config
	if config.ChunkSize == 0 {
		config = rag.DefaultKnowledgeBaseConfig()
	}

	kb := &rag.KnowledgeBase{
		ID:           uuid.New().String(),
		WorkspaceID:  input.WorkspaceID,
		DepartmentID: departmentID,
		Name:         input.Name,
		Description:  input.Description,
		Status:       rag.KnowledgeBaseStatusActive,
		Config:       config,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	kb.Normalize()

	if err := kb.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Create(ctx, kb); err != nil {
		return nil, err
	}

	return kb, nil
}
