package cart_usecase

import (
	"vozko/domain/cart"
)

type clearCartUseCase struct {
	cartRepo cart.CartRepository
}

func NewClearCartUseCase(cartRepo cart.CartRepository) cart.ClearCartUseCase {
	return &clearCartUseCase{
		cartRepo: cartRepo,
	}
}

func (uc *clearCartUseCase) Execute(userID string) error {
	return uc.cartRepo.ClearCart(userID)
}
