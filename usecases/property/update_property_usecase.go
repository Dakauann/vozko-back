package property_usecase

import (
	"vozko/domain/property"
	"vozko/domain/shop"
)

type updatePropertyUseCase struct {
	repo     property.PropertyRepository
	shopRepo shop.Repository
}

func NewUpdatePropertyUseCase(repo property.PropertyRepository, shopRepo shop.Repository) property.UpdatePropertyUseCase {
	return &updatePropertyUseCase{repo: repo, shopRepo: shopRepo}
}

func (uc *updatePropertyUseCase) Execute(userID string, propertyID string, p *property.Property) error {
	existing, err := uc.repo.FindByID(propertyID)
	if err != nil {
		return err
	}

	propertyShop, err := uc.shopRepo.FindByID(existing.ShopID)
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

	return uc.repo.Update(propertyID, p)
}
