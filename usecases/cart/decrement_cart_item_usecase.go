package cart_usecase

import (
	"vozko/domain/cart"
)

type decrementCartItemUseCase struct {
	cartRepo cart.CartRepository
}

func NewDecrementCartItemUseCase(cartRepo cart.CartRepository) cart.DecrementCartItemUseCase {
	return &decrementCartItemUseCase{
		cartRepo: cartRepo,
	}
}

func (uc *decrementCartItemUseCase) Execute(userID string, itemID string) (*cart.Cart, error) {
	cartItem, err := uc.cartRepo.GetCartItemByID(userID, itemID)
	if err != nil {
		return nil, cart.ErrCartItemNotFound
	}

	newQuantity := cartItem.Quantity - 1

	if newQuantity <= 0 {
		err = uc.cartRepo.RemoveCartItem(userID, itemID)
		if err != nil {
			return nil, err
		}
	} else {
		unitPrice := cartItem.UnitPrice

		err = uc.cartRepo.UpdateCartItem(userID, itemID, newQuantity, unitPrice)
		if err != nil {
			return nil, err
		}
	}

	return uc.cartRepo.GetCartByUserID(userID)
}
