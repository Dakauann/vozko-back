package product

import "vozko/domain/shared"

type ListProductsInput struct {
	Search  string
	Options shared.QueryOptions
}

type SearchProductsInput struct {
	Query   string
	Options shared.QueryOptions
}
