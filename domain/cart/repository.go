package cart

type CartRepository interface {
	GetCartByUserID(userID string) (*Cart, error)
	CreateCart(cart *Cart) error
	UpdateCart(cart *Cart) error
	AddItemToCart(userID string, item *CartItem) error
	UpdateCartItem(userID string, itemID string, quantity int, unitPrice float64) error
	RemoveCartItem(userID string, itemID string) error
	RemoveCartItems(userID string, itemIDs []string) error
	ClearCart(userID string) error
	GetCartItemByID(userID string, itemID string) (*CartItem, error)
	GetCartItemsByIDs(userID string, itemIDs []string) ([]CartItem, error)
	GetCartItemByVariantAndOptions(userID string, variantID string, options []SelectedOption) (*CartItem, error)
	GetCartItemByVariant(userID string, variantID string) (*CartItem, error)
}
