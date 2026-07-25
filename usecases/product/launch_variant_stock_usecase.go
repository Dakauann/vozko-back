package product_usecase

import (
	"vozko/domain/inventory"
	"vozko/domain/product"
)

type launchVariantStockUseCase struct {
	productRepo  product.ProductRepository
	stockService inventory.VariantStockService
}

func NewLaunchVariantStockUseCase(productRepo product.ProductRepository, stockService inventory.VariantStockService) product.LaunchVariantStockUseCase {
	return &launchVariantStockUseCase{
		productRepo:  productRepo,
		stockService: stockService,
	}
}

func (uc *launchVariantStockUseCase) Execute(userID string, productID string, variantID string, quantity int, note string) error {
	if quantity <= 0 {
		return product.ErrVariantStockAdjustmentInvalid
	}

	p, err := uc.productRepo.FindByID(productID)
	if err != nil {
		return err
	}
	if p == nil {
		return product.ErrProductNotFound
	}

	var targetVariant *product.Variant
	for i := range p.Variants {
		if p.Variants[i].ID == variantID {
			targetVariant = &p.Variants[i]
			break
		}
	}
	if targetVariant == nil {
		return product.ErrVariantNotFound
	}

	metadata := inventory.VariantStockMetadata{
		ActorID:   userID,
		ProductID: productID,
		Note:      note,
	}

	_, err = uc.stockService.RecordLaunch(variantID, quantity, metadata)
	return err
}
