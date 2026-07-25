package product_usecase

import (
	"strings"

	"vozko/domain/category"
	"vozko/domain/product"
	"vozko/domain/shop"
)

type createProductUseCase struct {
	repo         product.ProductRepository
	categoryRepo category.Repository
	shopRepo     shop.Repository
}

func NewCreateProductUseCase(repo product.ProductRepository, categoryRepo category.Repository, shopRepo shop.Repository) product.CreateProductUseCase {
	return &createProductUseCase{repo: repo, categoryRepo: categoryRepo, shopRepo: shopRepo}
}

func (uc *createProductUseCase) Execute(userID string, p *product.Product) error {
	if err := uc.validateProduct(userID, p); err != nil {
		return err
	}
	return uc.repo.Create(p)
}

func (uc *createProductUseCase) validateProduct(userID string, p *product.Product) error {
	shopExists, err := uc.shopRepo.FindByID(p.ShopID)

	if err != nil {
		return product.ErrProductShopNotFound
	}

	if shopExists.UserID != userID {
		return product.ErrProductShopUnauthorized
	}

	if p.Name == "" {
		return product.ErrProductNameRequired
	}
	if len(p.Name) < 3 {
		return product.ErrProductNameTooShort
	}
	if len(p.Name) > 255 {
		return product.ErrProductNameTooLong
	}

	if p.Description == "" {
		return product.ErrProductDescriptionRequired
	}
	if len(p.Description) < 10 {
		return product.ErrProductDescriptionTooShort
	}

	if len(p.Variants) == 0 {
		return product.ErrProductVariantsRequired
	}

	for i := range p.Variants {
		variant := &p.Variants[i]
		if len(variant.MediaIDs) == 0 {
			return product.ErrVariantMediaRequired
		}
		for _, mediaID := range variant.MediaIDs {
			exists, err := uc.repo.MediaExists(mediaID)
			if err != nil {
				return err
			}
			if !exists {
				return product.ErrMediaNotFound
			}
		}
	}

	for i := range p.Variants {
		variant := &p.Variants[i]

		if variant.SKU == "" {
			return product.ErrVariantSKURequired
		}

		if variant.Name == "" {
			return product.ErrVariantNameRequired
		}
		if len(variant.Name) > 255 {
			return product.ErrVariantNameTooLong
		}
		if len(variant.SKU) > 50 {
			return product.ErrVariantSKUTooLong
		}
		if variant.RetailPrice == nil || *variant.RetailPrice <= 0 {
			return product.ErrVariantRetailPriceInvalid
		}
		if variant.WholesalePrice == nil || *variant.WholesalePrice <= 0 {
			return product.ErrVariantWholesalePriceInvalid
		}
		if variant.Cost == nil || *variant.Cost <= 0 {
			return product.ErrVariantCostInvalid
		}
		if variant.Inventory <= 0 {
			return product.ErrVariantInventoryInvalid
		}
		if variant.WeightKg == nil || *variant.WeightKg <= 0 {
			return product.ErrVariantWeightInvalid
		}
		if variant.HeightCm == nil || *variant.HeightCm <= 0 {
			return product.ErrVariantDimensionsInvalid
		}
		if variant.WidthCm == nil || *variant.WidthCm <= 0 {
			return product.ErrVariantDimensionsInvalid
		}
		if variant.DepthCm == nil || *variant.DepthCm <= 0 {
			return product.ErrVariantDimensionsInvalid
		}
		if variant.MinQuantityForWholesale == nil || *variant.MinQuantityForWholesale <= 0 {
			return product.ErrVariantMinQuantityInvalid
		}

		var categoryID string
		if variant.CategoryID != nil {
			categoryID = strings.TrimSpace(*variant.CategoryID)
		}
		if categoryID == "" {
			return product.ErrVariantCategoryRequired
		}
		exists, err := uc.categoryRepo.Exists(categoryID)
		if err != nil {
			return err
		}
		if !exists {
			return product.ErrVariantCategoryNotFound
		}

		hasChildren, err := uc.categoryRepo.HasChildren(categoryID)
		if err != nil {
			return err
		}
		if hasChildren {
			return product.ErrVariantCategoryMustBeLeaf
		}
		variant.CategoryID = &categoryID

		for _, option := range variant.Options {
			if option.OptionValueID == "" {
				if option.OptionType != "" && option.OptionValue != "" {

					opValueID, err := uc.repo.FindOrCreateOptionValue(option.OptionType, option.OptionValue)
					if err != nil {
						return err
					}
					option.OptionValueID = opValueID
				} else {
					return product.ErrVariantOptionValueRequired
				}
			}

			exists, err := uc.repo.OptionValueExists(option.OptionValueID)
			if err != nil {
				return err
			}
			if !exists {
				return product.ErrVariantOptionValueRequired
			}
		}
	}

	return nil
}
