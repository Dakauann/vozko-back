package category

import "vozko/domain/shared"

type ListCategoriesInput struct {
	Search   string
	ParentID *string
	Options  shared.QueryOptions
}
