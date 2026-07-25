package cart_usecase

import (
	"vozko/domain/cart"
)

type getCartUseCase struct {
	cartRepo cart.CartRepository
}

func NewGetCartUseCase(cartRepo cart.CartRepository) cart.GetCartUseCase {
	return &getCartUseCase{
		cartRepo: cartRepo,
	}
}

func (uc *getCartUseCase) Execute(userID string) (*cart.Cart, error) {
	cartResult, err := uc.cartRepo.GetCartByUserID(userID)
	if err != nil {
		return nil, err
	}

	if cartResult == nil {
		return &cart.Cart{
			UserID: userID,
			Items:  []cart.CartItem{},
		}, nil
	}

	return cartResult, nil
}
