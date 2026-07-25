package shop_usecase

import "vozko/domain/shop"

type deleteShopUseCase struct {
	repo shop.Repository
}

func NewDeleteShopUseCase(repo shop.Repository) shop.DeleteShopUseCase {
	return &deleteShopUseCase{repo: repo}
}

func (uc *deleteShopUseCase) Execute(shopID int64) error {
	_, err := uc.repo.FindByID(shopID)
	if err != nil {
		return err
	}

	return uc.repo.Delete(shopID)
}
