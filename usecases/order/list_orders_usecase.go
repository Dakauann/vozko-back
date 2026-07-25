package order_usecase

import (
	"vozko/domain/order"
	"vozko/domain/shared"
)

type listOrdersUseCase struct {
	repo order.OrderRepository
}

func NewListOrdersUseCase(repo order.OrderRepository) order.ListOrdersUseCase {
	return &listOrdersUseCase{repo: repo}
}

func (uc *listOrdersUseCase) Execute(input order.ListOrdersInput) (*shared.PaginatedResult[*order.Order], error) {
	return uc.repo.ListByWorkspace(input)
}
