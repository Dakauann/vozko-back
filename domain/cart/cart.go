package cart

import (
	"errors"
	"time"
	"vozko/domain/media"
)

var (
	ErrCartItemNotFound        = errors.New("cart item not found")
	ErrInvalidQuantity         = errors.New("quantity must be greater than 0")
	ErrInsufficientStock       = errors.New("insufficient stock available")
	ErrVariantNotFound         = errors.New("product variant not found")
	ErrVariantNotAnnounced     = errors.New("product variant is not available for sale")
	ErrProductNotFound         = errors.New("product not found")
	ErrCartEmpty               = errors.New("cart is empty")
	ErrMaxQuantityExceeded     = errors.New("maximum quantity per item exceeded")
	ErrOptionSelectionRequired = errors.New("option selection is required for this variant")
	ErrInvalidOptionSelection  = errors.New("invalid option type or value for this variant")
)

const (
	MaxQuantityPerItemForIndividuals = 10
)

type Cart struct {
	ID        string     `json:"id"`
	UserID    string     `json:"userId"`
	Items     []CartItem `json:"items"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type CartItem struct {
	ID              string              `json:"id"`
	CartID          string              `json:"cartId"`
	ProductID       string              `json:"productId"`
	VariantID       string              `json:"variantId"`
	Quantity        int                 `json:"quantity"`
	UnitPrice       float64             `json:"unitPrice"`
	TotalPrice      float64             `json:"totalPrice"`
	SelectedOptions []CartVariantOption `json:"options,omitempty"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	Product         *CartProduct        `json:"product,omitempty"`
}

type CartProduct struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Variant     *CartVariant  `json:"variant,omitempty"`
	Medias      []media.Media `json:"medias,omitempty"`
}

type CartVariant struct {
	ID                      string              `json:"id"`
	SKU                     string              `json:"sku"`
	Name                    string              `json:"name"`
	RetailPrice             float64             `json:"retailPrice"`
	WholesalePrice          float64             `json:"wholesalePrice"`
	Announced               bool                `json:"announced"`
	MinQuantityForWholesale int                 `json:"minQuantityForWholesale"`
	Inventory               int                 `json:"inventory"`
	Options                 []CartVariantOption `json:"options,omitempty"`
}

type CartVariantOption struct {
	OptionType  string `json:"optionType"`
	OptionValue string `json:"optionValue"`
}
