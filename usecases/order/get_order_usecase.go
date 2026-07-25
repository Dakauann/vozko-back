package order_usecase

import "vozko/domain/order"

type getOrderUseCase struct {
	repo order.OrderRepository
}

func NewGetOrderUseCase(repo order.OrderRepository) order.GetOrderUseCase {
	return &getOrderUseCase{repo: repo}
}

func (uc *getOrderUseCase) Execute(userID string, orderID string) (*order.Order, error) {
	return uc.repo.GetByID(userID, orderID)
}
