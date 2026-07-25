package cart_usecase

import (
	"time"
	"vozko/domain/cart"
	"vozko/domain/payment"
	"vozko/domain/product"
	"vozko/domain/user"
)

type addToCartUseCase struct {
	cartRepo       cart.CartRepository
	productRepo    product.ProductRepository
	userRepo       user.UserRepository
	pricingService payment.PricingService
}

func NewAddToCartUseCase(cartRepo cart.CartRepository, productRepo product.ProductRepository, userRepo user.UserRepository, pricingService payment.PricingService) cart.AddToCartUseCase {
	return &addToCartUseCase{
		cartRepo:       cartRepo,
		productRepo:    productRepo,
		userRepo:       userRepo,
		pricingService: pricingService,
	}
}

func (uc *addToCartUseCase) Execute(userID string, productID string, variantID string, quantity int, selectedOptions []cart.SelectedOption) (*cart.Cart, error) {
	if quantity <= 0 {
		return nil, cart.ErrInvalidQuantity
	}

	productToAdd, err := uc.productRepo.FindByID(productID)
	if err != nil {
		return nil, err
	}

	if productToAdd == nil {
		return nil, cart.ErrProductNotFound
	}

	dbUser, err := uc.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}

	targetProduct := productToAdd

	var targetVariant *product.Variant
	for i := range targetProduct.Variants {
		if targetProduct.Variants[i].ID == variantID {
			targetVariant = &targetProduct.Variants[i]
			break
		}
	}

	if targetVariant == nil {
		return nil, cart.ErrVariantNotFound
	}

	if !targetVariant.Announced {
		return nil, cart.ErrVariantNotAnnounced
	}

	if len(targetVariant.Options) > 0 {
		if len(selectedOptions) == 0 {
			return nil, cart.ErrOptionSelectionRequired
		}

		availableOptions := make(map[string]map[string]bool)
		for _, opt := range targetVariant.Options {
			if availableOptions[opt.OptionType] == nil {
				availableOptions[opt.OptionType] = make(map[string]bool)
			}
			availableOptions[opt.OptionType][opt.OptionValue] = true
		}

		requiredTypes := make(map[string]bool)
		for optType := range availableOptions {
			requiredTypes[optType] = true
		}

		providedTypes := make(map[string]bool)
		for _, selected := range selectedOptions {
			providedTypes[selected.OptionType] = true

			validValues, exists := availableOptions[selected.OptionType]
			if !exists {
				return nil, cart.ErrInvalidOptionSelection
			}

			if !validValues[selected.OptionValue] {
				return nil, cart.ErrInvalidOptionSelection
			}
		}

		for optType := range requiredTypes {
			if !providedTypes[optType] {
				return nil, cart.ErrOptionSelectionRequired
			}
		}
	}

	if targetVariant.Inventory < quantity {
		return nil, cart.ErrInsufficientStock
	}

	existingItem, err := uc.cartRepo.GetCartItemByVariantAndOptions(userID, variantID, selectedOptions)
	if err != nil {
		return nil, err
	}

	if existingItem != nil {
		newQuantity := existingItem.Quantity + quantity
		if newQuantity > targetVariant.Inventory {
			return nil, cart.ErrInsufficientStock
		}

		unitPrice, err := uc.pricingService.CalculateUnitPrice(dbUser, targetVariant, newQuantity)

		err = uc.cartRepo.UpdateCartItem(userID, existingItem.ID, newQuantity, unitPrice)
		if err != nil {
			return nil, err
		}
	} else {
		selectedCartOptions := make([]cart.CartVariantOption, len(selectedOptions))
		for i, opt := range selectedOptions {
			selectedCartOptions[i] = cart.CartVariantOption{
				OptionType:  opt.OptionType,
				OptionValue: opt.OptionValue,
			}
		}

		unitPrice, err := uc.pricingService.CalculateUnitPrice(dbUser, targetVariant, quantity)
		if err != nil {
			return nil, err
		}

		totalPrice := unitPrice * float64(quantity)

		cartItem := &cart.CartItem{
			ProductID:       productID,
			VariantID:       variantID,
			Quantity:        quantity,
			UnitPrice:       unitPrice,
			TotalPrice:      totalPrice,
			SelectedOptions: selectedCartOptions,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		cartItem.TotalPrice = unitPrice * float64(quantity)

		err = uc.cartRepo.AddItemToCart(userID, cartItem)
		if err != nil {
			return nil, err
		}
	}

	return uc.cartRepo.GetCartByUserID(userID)
}
