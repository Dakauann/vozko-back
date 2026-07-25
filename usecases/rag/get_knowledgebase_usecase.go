package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type getKnowledgeBaseUseCase struct {
	repo rag.KnowledgeBaseRepository
}

func NewGetKnowledgeBaseUseCase(repo rag.KnowledgeBaseRepository) rag.GetKnowledgeBaseUseCase {
	return &getKnowledgeBaseUseCase{repo: repo}
}

func (uc *getKnowledgeBaseUseCase) Execute(ctx context.Context, id string) (*rag.KnowledgeBase, error) {
	return uc.repo.FindByID(ctx, id)
}
