package cart_usecase

import (
	"vozko/domain/cart"
)

type removeFromCartUseCase struct {
	cartRepo cart.CartRepository
}

func NewRemoveFromCartUseCase(cartRepo cart.CartRepository) cart.RemoveFromCartUseCase {
	return &removeFromCartUseCase{
		cartRepo: cartRepo,
	}
}

func (uc *removeFromCartUseCase) Execute(userID string, itemID string) (*cart.Cart, error) {
	_, err := uc.cartRepo.GetCartItemByID(userID, itemID)
	if err != nil {
		return nil, cart.ErrCartItemNotFound
	}

	err = uc.cartRepo.RemoveCartItem(userID, itemID)
	if err != nil {
		return nil, err
	}

	return uc.cartRepo.GetCartByUserID(userID)
}
