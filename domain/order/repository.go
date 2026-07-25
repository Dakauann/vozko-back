package order

import "vozko/domain/shared"

type InventoryUpdate struct {
	VariantID string
	Quantity  int
}

type OrderRepository interface {
	Create(order *Order) error
	CreateWithInventoryReservation(order *Order, inventoryUpdates []InventoryUpdate) error
	Update(order *Order) error
	UpdateStatus(orderID string, status Status) error
	GetByID(userID string, orderID string) (*Order, error)
	GetByIDForSystem(orderID string) (*Order, error)
	ListByWorkspace(input ListOrdersInput) (*shared.PaginatedResult[*Order], error)
	GetExpiredOrders() ([]*Order, error)
	CancelOrder(orderID string) error
	CancelExpiredOrdersAndReturnInventory(orderIDs []string) error
}

type GetOrderUseCase interface {
	Execute(userID string, orderID string) (*Order, error)
}

type ListOrdersUseCase interface {
	Execute(input ListOrdersInput) (*shared.PaginatedResult[*Order], error)
}

type CancelExpiredOrderUseCase interface {
	Execute() error
}
