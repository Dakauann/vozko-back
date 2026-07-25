package product_usecase

import (
	"vozko/domain/product"
	"vozko/domain/shared"
)

type listProductsUseCase struct {
	repo product.ProductRepository
}

func NewListProductsUseCase(repo product.ProductRepository) product.ListProductsUseCase {
	return &listProductsUseCase{
		repo: repo,
	}
}

func (uc *listProductsUseCase) Execute(input product.ListProductsInput) (*shared.PaginatedResult[*product.Product], error) {
	return uc.repo.List(input)
}
