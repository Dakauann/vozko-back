package rag_usecase

import (
	"context"

	"vozko/domain/rag"
	"vozko/domain/shared"
)

type listDocumentsUseCase struct {
	repo rag.DocumentRepository
}

func NewListDocumentsUseCase(repo rag.DocumentRepository) rag.ListDocumentsUseCase {
	return &listDocumentsUseCase{repo: repo}
}

func (uc *listDocumentsUseCase) Execute(ctx context.Context, knowledgeBaseID string, page int, pageSize int) (*rag.DocumentListOutput, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	pagination := shared.Pagination{
		Page:     page,
		PageSize: pageSize,
	}

	result, err := uc.repo.FindByKnowledgeBase(ctx, knowledgeBaseID, pagination)
	if err != nil {
		return nil, err
	}

	totalPages := int(result.TotalItems) / pageSize
	if int(result.TotalItems)%pageSize > 0 {
		totalPages++
	}

	return &rag.DocumentListOutput{
		Items:      result.Items,
		Total:      result.TotalItems,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}
