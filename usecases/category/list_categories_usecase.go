package category_usecase

import (
	"vozko/domain/category"
	"vozko/domain/shared"
)

type listCategoriesUseCase struct {
	repo category.Repository
}

func NewListCategoriesUseCase(repo category.Repository) category.ListCategoriesUseCase {
	return &listCategoriesUseCase{repo: repo}
}

func (uc *listCategoriesUseCase) Execute(input category.ListCategoriesInput) (*shared.PaginatedResult[*category.Category], error) {
	if input.ParentID == nil && input.Search == "" {
		rootFilter := ""
		input.ParentID = &rootFilter
	}

	return uc.repo.List(input)
}
