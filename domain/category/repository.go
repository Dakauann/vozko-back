package category

import "vozko/domain/shared"

type Repository interface {
	Create(category *Category) error
	Update(category *Category) error
	Delete(id string) error
	FindByID(id string) (*Category, error)
	List(input ListCategoriesInput) (*shared.PaginatedResult[*Category], error)
	Exists(id string) (bool, error)
	SlugExists(slug string, excludeID *string) (bool, error)
	HasChildren(id string) (bool, error)
	HasLinkedProducts(id string) (bool, error)
	GetDescendantIDs(id string) ([]string, error)
}
