package rag_usecase

import (
	"context"
	"time"

	"vozko/domain/rag"
)

type updateKnowledgeBaseUseCase struct {
	repo rag.KnowledgeBaseRepository
}

func NewUpdateKnowledgeBaseUseCase(repo rag.KnowledgeBaseRepository) rag.UpdateKnowledgeBaseUseCase {
	return &updateKnowledgeBaseUseCase{repo: repo}
}

func (uc *updateKnowledgeBaseUseCase) Execute(ctx context.Context, id string, input rag.UpdateKnowledgeBaseInput) (*rag.KnowledgeBase, error) {
	kb, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		kb.Name = *input.Name
	}
	if input.Description != nil {
		kb.Description = *input.Description
	}
	if input.Config != nil {
		kb.Config = *input.Config
	}

	kb.UpdatedAt = time.Now()
	kb.Normalize()

	if err := kb.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(ctx, kb); err != nil {
		return nil, err
	}

	return kb, nil
}
