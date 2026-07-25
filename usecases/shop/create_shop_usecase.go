package shop_usecase

import (
	"strings"

	"vozko/domain/media"
	"vozko/domain/shop"
)

type createShopUseCase struct {
	repo      shop.Repository
	mediaRepo media.MediaRepository
}

func NewCreateShopUseCase(repo shop.Repository, mediaRepo media.MediaRepository) shop.CreateShopUseCase {
	return &createShopUseCase{
		repo:      repo,
		mediaRepo: mediaRepo,
	}
}

func (uc *createShopUseCase) Execute(input shop.CreateShopInput) (*shop.Shop, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, shop.ErrShopNameRequired
	}
	if len(name) < 3 {
		return nil, shop.ErrShopNameTooShort
	}
	if len(name) > 255 {
		return nil, shop.ErrShopNameTooLong
	}

	brand := strings.TrimSpace(input.Brand)
	if brand == "" {
		return nil, shop.ErrShopBrandRequired
	}

	shopCount, err := uc.repo.CountByUserID(input.UserID)
	if err != nil {
		return nil, err
	}
	if shopCount >= shop.MaxShopsPerUser {
		return nil, shop.ErrShopLimitReached
	}

	if input.LogoMediaID == "" {
		return nil, shop.ErrShopLogoRequired
	}
	exists, err := uc.mediaRepo.MediaExists(input.LogoMediaID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, shop.ErrInvalidMediaID
	}

	if input.BannerMediaID != "" {
		exists, err := uc.mediaRepo.MediaExists(input.BannerMediaID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, shop.ErrInvalidMediaID
		}
	}

	newShop := &shop.Shop{
		UserID:        input.UserID,
		Name:          name,
		Brand:         strings.TrimSpace(input.Brand),
		LogoMediaID:   input.LogoMediaID,
		BannerMediaID: input.BannerMediaID,
	}

	if err := uc.repo.Create(newShop); err != nil {
		return nil, err
	}

	return newShop, nil
}
