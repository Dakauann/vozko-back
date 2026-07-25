package payment

import (
	"vozko/domain/product"
	"vozko/domain/user"
)

type PricingService interface {
	CalculateUnitPrice(user *user.User, variant *product.Variant, quantity int) (float64, error)
}
