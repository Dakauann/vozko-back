package product

import "vozko/domain/shared"

type CreateProductUseCase interface {
	Execute(userID string, p *Product) error
}

type UpdateProductUseCase interface {
	Execute(userID string, productID string, input *UpdateProductInput) error
}

type GetProductUseCase interface {
	Execute(productID string) (*Product, error)
}

type ListProductsUseCase interface {
	Execute(input ListProductsInput) (*shared.PaginatedResult[*Product], error)
}

type SearchProductsUseCase interface {
	Execute(input SearchProductsInput) (*shared.PaginatedResult[*Product], error)
}

type LaunchVariantStockUseCase interface {
	Execute(userID string, productID string, variantID string, quantity int, note string) error
}
