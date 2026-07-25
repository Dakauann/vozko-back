package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type getDocumentUseCase struct {
	repo rag.DocumentRepository
}

func NewGetDocumentUseCase(repo rag.DocumentRepository) rag.GetDocumentUseCase {
	return &getDocumentUseCase{repo: repo}
}

func (uc *getDocumentUseCase) Execute(ctx context.Context, id string) (*rag.Document, error) {
	return uc.repo.FindByID(ctx, id)
}
