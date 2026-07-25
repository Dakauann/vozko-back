package pricing_service

import (
	"fmt"
	"vozko/domain/payment"
	"vozko/domain/product"
	"vozko/domain/user"
)

type pricingService struct {
}

func NewPricingService() payment.PricingService {
	return &pricingService{}
}

func (p *pricingService) CalculateUnitPrice(buyer *user.User, variant *product.Variant, quantity int) (float64, error) {
	retailPrice := float64(0)
	wholesalePrice := float64(0)
	minQty := 0

	if variant.RetailPrice != nil {
		retailPrice = *variant.RetailPrice
	}
	if variant.WholesalePrice != nil {
		wholesalePrice = *variant.WholesalePrice
	}
	if variant.MinQuantityForWholesale != nil {
		minQty = *variant.MinQuantityForWholesale
	}

	unitPrice := retailPrice

	if buyer.CustomerType == user.CustomerTypeCompany {
		if minQty > 0 && quantity >= minQty {
			unitPrice = wholesalePrice
		}
	}

	fmt.Printf("Calculated unit price: %.2f for user type: %s, quantity: %d\n", unitPrice, buyer.CustomerType, quantity)

	return unitPrice, nil
}
