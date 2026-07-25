package category

import "vozko/domain/shared"

type CreateCategoryUseCase interface {
	Execute(input CreateCategoryInput) (*Category, error)
}

type UpdateCategoryUseCase interface {
	Execute(id string, input UpdateCategoryInput) (*Category, error)
}

type DeleteCategoryUseCase interface {
	Execute(id string) error
}

type GetCategoryUseCase interface {
	Execute(id string) (*Category, error)
}

type ListCategoriesUseCase interface {
	Execute(input ListCategoriesInput) (*shared.PaginatedResult[*Category], error)
}

type CreateCategoryInput struct {
	Name        string
	Slug        string
	Description string
	ParentID    *string
}

type UpdateCategoryInput struct {
	Name        *string
	Slug        *string
	Description *string
	ParentID    *string
	ClearParent bool
}
