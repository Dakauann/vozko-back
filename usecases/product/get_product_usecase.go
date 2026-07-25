package product_usecase

import (
	"vozko/domain/product"
)

type getProductUseCase struct {
	productRepository product.ProductRepository
}

func NewGetProductUseCase(productRepository product.ProductRepository) product.GetProductUseCase {
	return &getProductUseCase{
		productRepository: productRepository,
	}
}

func (uc *getProductUseCase) Execute(productID string) (*product.Product, error) {
	return uc.productRepository.FindByID(productID)
}
