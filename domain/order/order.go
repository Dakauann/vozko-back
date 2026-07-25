package order

import (
	"errors"
	"time"

	"vozko/domain/payment"
	"vozko/domain/ticket"
)

var (
	ErrOrderNotFound           = errors.New("order not found")
	ErrInvalidOrderData        = errors.New("invalid order data")
	ErrCartEmpty               = errors.New("cart is empty")
	ErrAddressNotFound         = errors.New("address not found")
	ErrProductNotFound         = errors.New("product not found")
	ErrVariantNotFound         = errors.New("variant not found")
	ErrVariantNotAnnounced     = errors.New("variant is no longer available")
	ErrProductOutOfStock       = errors.New("product out of stock")
	ErrInsufficientStock       = errors.New("insufficient stock available")
	ErrInvalidCustomerDocument = errors.New("invalid customer document")
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusPaid       Status = "paid"
	StatusShipped    Status = "shipped"
	StatusDelivered  Status = "delivered"
	StatusCancelled  Status = "cancelled"
	StatusRefunded   Status = "refunded"
)

type Order struct {
	ID                string           `json:"id"`
	UserID            string           `json:"userId"`
	ShippingAddressID string           `json:"shippingAddressId"`
	CustomerName      string           `json:"customerName"`
	CustomerDocument  string           `json:"customerDocument"`
	Items             []OrderItem      `json:"items"`
	Payment           *payment.Payment `json:"payment,omitempty"`
	Ticket            *ticket.Ticket   `json:"ticket,omitempty"`
	TotalAmount       float64          `json:"totalAmount"`
	Status            Status           `json:"status"`
	ExpiresAt         *time.Time       `json:"expiresAt,omitempty"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
}

type OrderItem struct {
	ID         string            `json:"id"`
	OrderID    string            `json:"orderId"`
	ProductID  string            `json:"productId"`
	VariantID  string            `json:"variantId"`
	SKU        string            `json:"sku"`
	Quantity   int               `json:"quantity"`
	UnitPrice  float64           `json:"unitPrice"`
	TotalPrice float64           `json:"totalPrice"`
	Options    []OrderItemOption `json:"options,omitempty"`
}

type OrderItemOption struct {
	OptionType  string `json:"optionType"`
	OptionValue string `json:"optionValue"`
}

type DirectCheckoutItem struct {
	ProductID string            `json:"productId"`
	VariantID string            `json:"variantId"`
	Quantity  int               `json:"quantity"`
	Options   []OrderItemOption `json:"options,omitempty"`
}

type CheckoutRequest struct {
	AddressID        string              `json:"addressId"`
	CustomerName     string              `json:"customerName"`
	CustomerDocument string              `json:"customerDocument"`
	DirectItem       *DirectCheckoutItem `json:"directItem,omitempty"`
	CartItemIDs      []string            `json:"cartItemIds,omitempty"`
}

type CheckoutUseCase interface {
	Execute(userID string, request *CheckoutRequest) (*Order, error)
}
