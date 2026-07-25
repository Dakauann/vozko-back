package product_usecase

import (
	"strings"

	"vozko/domain/product"
	"vozko/domain/shared"
)

type searchProductsUseCase struct {
	repo product.ProductRepository
}

func NewSearchProductsUseCase(repo product.ProductRepository) product.SearchProductsUseCase {
	return &searchProductsUseCase{repo: repo}
}

func (uc *searchProductsUseCase) Execute(input product.SearchProductsInput) (*shared.PaginatedResult[*product.Product], error) {
	normalized := strings.TrimSpace(input.Query)
	options := input.Options
	options.Pagination = shared.NormalizePagination(options.Pagination)

	if normalized == "" {
		return uc.repo.List(product.ListProductsInput{
			Search:  "",
			Options: options,
		})
	}

	return uc.repo.Search(product.SearchProductsInput{
		Query:   normalized,
		Options: options,
	})
}
