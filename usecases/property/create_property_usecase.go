package property_usecase

import (
	"vozko/domain/property"
	"vozko/domain/shop"
)

type createPropertyUseCase struct {
	repo     property.PropertyRepository
	shopRepo shop.Repository
}

func NewCreatePropertyUseCase(repo property.PropertyRepository, shopRepo shop.Repository) property.CreatePropertyUseCase {
	return &createPropertyUseCase{repo: repo, shopRepo: shopRepo}
}

func (uc *createPropertyUseCase) Execute(userID string, p *property.Property) error {
	propertyShop, err := uc.shopRepo.FindByID(p.ShopID)
	if err != nil {
		return property.ErrPropertyShopNotFound
	}
	if propertyShop.UserID != userID {
		return property.ErrPropertyShopUnauthorized
	}

	if err := p.Validate(); err != nil {
		return err
	}

	p.CalculatePricePerSqMeter()

	return uc.repo.Create(p)
}
