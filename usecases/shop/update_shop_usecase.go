package shop_usecase

import (
	"strings"

	"vozko/domain/media"
	"vozko/domain/shop"
)

type updateShopUseCase struct {
	repo      shop.Repository
	mediaRepo media.MediaRepository
}

func NewUpdateShopUseCase(repo shop.Repository, mediaRepo media.MediaRepository) shop.UpdateShopUseCase {
	return &updateShopUseCase{
		repo:      repo,
		mediaRepo: mediaRepo,
	}
}

func (uc *updateShopUseCase) Execute(shopID int64, input shop.UpdateShopInput) (*shop.Shop, error) {
	existing, err := uc.repo.FindByID(shopID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, shop.ErrShopNameRequired
		}
		if len(name) < 3 {
			return nil, shop.ErrShopNameTooShort
		}
		if len(name) > 255 {
			return nil, shop.ErrShopNameTooLong
		}
		existing.Name = name
	}

	if input.Brand != nil {
		existing.Brand = strings.TrimSpace(*input.Brand)
	}

	if input.LogoMediaID != nil {
		logoID := strings.TrimSpace(*input.LogoMediaID)
		if logoID != "" {
			exists, err := uc.mediaRepo.MediaExists(logoID)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, shop.ErrInvalidMediaID
			}
		}
		existing.LogoMediaID = logoID
	}

	if input.BannerMediaID != nil {
		bannerID := strings.TrimSpace(*input.BannerMediaID)
		if bannerID != "" {
			exists, err := uc.mediaRepo.MediaExists(bannerID)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, shop.ErrInvalidMediaID
			}
		}
		existing.BannerMediaID = bannerID
	}

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(shopID)
}
