package shop

import "vozko/domain/shared"

type Repository interface {
	Create(shop *Shop) error
	Update(shop *Shop) error
	Delete(id int64) error
	FindByID(id int64) (*Shop, error)
	FindByUserID(userID string) (*Shop, error)
	List(input ListShopsInput) (*shared.PaginatedResult[*Shop], error)
	Exists(id int64) (bool, error)
	UserHasShop(userID string) (bool, error)
	CountByUserID(userID string) (int64, error)
}

type ListShopsInput struct {
	Page     int
	PageSize int
	UserID   *string
	Search   *string
}

type CreateShopUseCase interface {
	Execute(input CreateShopInput) (*Shop, error)
}

type UpdateShopUseCase interface {
	Execute(shopID int64, input UpdateShopInput) (*Shop, error)
}

type GetShopUseCase interface {
	Execute(shopID int64) (*Shop, error)
}

type ListShopsUseCase interface {
	Execute(input ListShopsInput) (*shared.PaginatedResult[*Shop], error)
}

type DeleteShopUseCase interface {
	Execute(shopID int64) error
}

type CreateShopInput struct {
	UserID        string
	Name          string
	Brand         string
	LogoMediaID   string
	BannerMediaID string
}

type UpdateShopInput struct {
	Name          *string
	Brand         *string
	LogoMediaID   *string
	BannerMediaID *string
}
