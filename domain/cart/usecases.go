package cart

type AddToCartUseCase interface {
	Execute(userID string, productID string, variantID string, quantity int, selectedOptions []SelectedOption) (*Cart, error)
}

type SelectedOption struct {
	OptionType  string `json:"optionType"`
	OptionValue string `json:"optionValue"`
}

type RemoveFromCartUseCase interface {
	Execute(userID string, itemID string) (*Cart, error)
}

type UpdateCartItemUseCase interface {
	Execute(userID string, itemID string, quantity int) (*Cart, error)
}

type GetCartUseCase interface {
	Execute(userID string) (*Cart, error)
}

type ClearCartUseCase interface {
	Execute(userID string) error
}

type DecrementCartItemUseCase interface {
	Execute(userID string, itemID string) (*Cart, error)
}
