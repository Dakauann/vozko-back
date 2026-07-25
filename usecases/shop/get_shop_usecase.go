package shop_usecase

import "vozko/domain/shop"

type getShopUseCase struct {
	repo shop.Repository
}

func NewGetShopUseCase(repo shop.Repository) shop.GetShopUseCase {
	return &getShopUseCase{repo: repo}
}

func (uc *getShopUseCase) Execute(shopID int64) (*shop.Shop, error) {
	return uc.repo.FindByID(shopID)
}
