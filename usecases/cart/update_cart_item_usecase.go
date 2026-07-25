package cart_usecase

import (
	"vozko/domain/cart"
	"vozko/domain/payment"
	"vozko/domain/product"
	"vozko/domain/user"
)

type updateCartItemUseCase struct {
	cartRepo       cart.CartRepository
	productRepo    product.ProductRepository
	userRepo       user.UserRepository
	pricingService payment.PricingService
}

func NewUpdateCartItemUseCase(cartRepo cart.CartRepository, productRepo product.ProductRepository, userRepo user.UserRepository, pricingService payment.PricingService) cart.UpdateCartItemUseCase {
	return &updateCartItemUseCase{
		cartRepo:       cartRepo,
		productRepo:    productRepo,
		userRepo:       userRepo,
		pricingService: pricingService,
	}
}

func (uc *updateCartItemUseCase) Execute(userID string, itemID string, quantity int) (*cart.Cart, error) {
	if quantity <= 0 {
		return nil, cart.ErrInvalidQuantity
	}

	cartItem, err := uc.cartRepo.GetCartItemByID(userID, itemID)
	if err != nil {
		return nil, cart.ErrCartItemNotFound
	}

	products, err := uc.productRepo.FindByIDs([]string{cartItem.ProductID})
	if err != nil {
		return nil, err
	}

	if len(products) == 0 {
		return nil, cart.ErrVariantNotFound
	}

	targetProduct := products[0]

	var targetVariant *product.Variant
	for i := range targetProduct.Variants {
		if targetProduct.Variants[i].ID == cartItem.VariantID {
			targetVariant = &targetProduct.Variants[i]
			break
		}
	}

	if targetVariant == nil {
		return nil, cart.ErrVariantNotFound
	}

	if targetVariant.Inventory < quantity {
		return nil, cart.ErrInsufficientStock
	}

	dbUser, err := uc.userRepo.FindByID(userID)

	if err != nil {
		return nil, err
	}

	unitPrice, err := uc.pricingService.CalculateUnitPrice(dbUser, targetVariant, quantity)

	err = uc.cartRepo.UpdateCartItem(userID, itemID, quantity, unitPrice)
	if err != nil {
		return nil, err
	}

	return uc.cartRepo.GetCartByUserID(userID)
}
