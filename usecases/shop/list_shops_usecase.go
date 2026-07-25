package shop_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/shop"
)

type listShopsUseCase struct {
	repo shop.Repository
}

func NewListShopsUseCase(repo shop.Repository) shop.ListShopsUseCase {
	return &listShopsUseCase{repo: repo}
}

func (uc *listShopsUseCase) Execute(input shop.ListShopsInput) (*shared.PaginatedResult[*shop.Shop], error) {
	return uc.repo.List(input)
}
