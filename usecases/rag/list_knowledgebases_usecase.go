package rag_usecase

import (
	"context"

	"vozko/domain/rag"
	"vozko/domain/shared"
)

type listKnowledgeBasesUseCase struct {
	repo rag.KnowledgeBaseRepository
}

func NewListKnowledgeBasesUseCase(repo rag.KnowledgeBaseRepository) rag.ListKnowledgeBasesUseCase {
	return &listKnowledgeBasesUseCase{repo: repo}
}

func (uc *listKnowledgeBasesUseCase) Execute(ctx context.Context, workspaceID string, departmentID *string, page int, pageSize int) (*rag.KnowledgeBaseListOutput, error) {
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

	var (
		result *shared.PaginatedResult[*rag.KnowledgeBase]
		err    error
	)
	if departmentID == nil {
		result, err = uc.repo.FindByWorkspace(ctx, workspaceID, pagination)
	} else {
		result, err = uc.repo.FindByWorkspaceAndDepartment(ctx, workspaceID, departmentID, pagination)
	}
	if err != nil {
		return nil, err
	}

	totalPages := int(result.TotalItems) / pageSize
	if int(result.TotalItems)%pageSize > 0 {
		totalPages++
	}

	return &rag.KnowledgeBaseListOutput{
		Items:      result.Items,
		Total:      result.TotalItems,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}
