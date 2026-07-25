package product_usecase

import (
	"strings"

	"vozko/domain/category"
	"vozko/domain/product"
	"vozko/domain/shop"
)

type updateProductUseCase struct {
	repo         product.ProductRepository
	categoryRepo category.Repository
	shopRepo     shop.Repository
}

func NewUpdateProductUseCase(repo product.ProductRepository, categoryRepo category.Repository, shopRepo shop.Repository) product.UpdateProductUseCase {
	return &updateProductUseCase{repo: repo, categoryRepo: categoryRepo, shopRepo: shopRepo}
}

func (uc *updateProductUseCase) Execute(userID string, productID string, input *product.UpdateProductInput) error {
	existing, err := uc.repo.FindByID(productID)
	if err != nil {
		return err
	}

	productShop, err := uc.shopRepo.FindByID(existing.ShopID)
	if err != nil {
		return product.ErrProductShopNotFound
	}
	if productShop.UserID != userID {
		return product.ErrProductShopUnauthorized
	}

	updated, err := uc.mergeProduct(existing, input)
	if err != nil {
		return err
	}

	return uc.repo.Update(productID, updated)
}

func (uc *updateProductUseCase) mergeProduct(existing *product.Product, input *product.UpdateProductInput) (*product.Product, error) {
	result := &product.Product{
		ID:          existing.ID,
		ShopID:      existing.ShopID,
		Name:        existing.Name,
		Description: existing.Description,
		Tags:        existing.Tags,
		Variants:    existing.Variants,
	}

	if err := uc.updateProductFields(result, input); err != nil {
		return nil, err
	}

	if len(input.Variants) > 0 {
		updatedVariants, err := uc.mergeVariants(existing.Variants, input.Variants)
		if err != nil {
			return nil, err
		}
		result.Variants = updatedVariants
	}

	return result, nil
}

func (uc *updateProductUseCase) updateProductFields(result *product.Product, input *product.UpdateProductInput) error {
	if name := strings.TrimSpace(input.Name); name != "" {
		if len(name) < 3 {
			return product.ErrProductNameTooShort
		}
		if len(name) > 255 {
			return product.ErrProductNameTooLong
		}
		result.Name = name
	}

	if desc := strings.TrimSpace(input.Description); desc != "" {
		if len(desc) < 10 {
			return product.ErrProductDescriptionTooShort
		}
		result.Description = desc
	}

	if len(input.Tags) > 0 {
		result.Tags = input.Tags
	}

	return nil
}

func (uc *updateProductUseCase) mergeVariants(existing []product.Variant, input []product.UpdateVariantInput) ([]product.Variant, error) {
	existingMap := make(map[string]product.Variant, len(existing))
	for _, v := range existing {
		existingMap[v.ID] = v
	}

	result := make([]product.Variant, 0, len(input))
	for i := range input {
		inputVariant := &input[i]

		variantID := strings.TrimSpace(inputVariant.ID)
		if variantID == "" {
			return nil, product.ErrVariantNotFound
		}

		original, ok := existingMap[variantID]
		if !ok {
			return nil, product.ErrVariantNotFound
		}

		merged, err := uc.mergeVariant(&original, inputVariant)
		if err != nil {
			return nil, err
		}

		result = append(result, *merged)
	}

	return result, nil
}

func (uc *updateProductUseCase) mergeVariant(existing *product.Variant, input *product.UpdateVariantInput) (*product.Variant, error) {
	result := *existing

	if err := uc.updateVariantFields(&result, input); err != nil {
		return nil, err
	}

	if err := uc.updateVariantCategory(&result, input); err != nil {
		return nil, err
	}

	if err := uc.updateVariantMedia(&result, input); err != nil {
		return nil, err
	}

	if err := uc.updateVariantOptions(&result, input); err != nil {
		return nil, err
	}

	return &result, nil
}

func (uc *updateProductUseCase) updateVariantFields(result *product.Variant, input *product.UpdateVariantInput) error {
	if input.Name != "" {
		if len(input.Name) > 255 {
			return product.ErrVariantNameTooLong
		}
		result.Name = input.Name
	}

	if input.SKU != "" {
		if len(input.SKU) > 50 {
			return product.ErrVariantSKUTooLong
		}
		result.SKU = input.SKU
	}

	if input.RetailPrice != nil {
		if *input.RetailPrice < 0 {
			return product.ErrVariantRetailPriceInvalid
		}
		result.RetailPrice = input.RetailPrice
	}

	if input.WholesalePrice != nil {
		if *input.WholesalePrice < 0 {
			return product.ErrVariantWholesalePriceInvalid
		}
		result.WholesalePrice = input.WholesalePrice
	}

	if input.Cost != nil {
		if *input.Cost < 0 {
			return product.ErrVariantCostInvalid
		}
		result.Cost = input.Cost
	}

	if input.Inventory != nil && *input.Inventory != result.Inventory {
		return product.ErrVariantInventoryUpdateNotAllowed
	}

	if input.MinQuantityForWholesale != nil {
		if *input.MinQuantityForWholesale < 0 {
			return product.ErrVariantMinQuantityInvalid
		}
		result.MinQuantityForWholesale = input.MinQuantityForWholesale
	}

	if input.WeightKg != nil {
		if *input.WeightKg < 0 {
			return product.ErrVariantWeightInvalid
		}
		result.WeightKg = input.WeightKg
	}

	if input.HeightCm != nil {
		if *input.HeightCm < 0 {
			return product.ErrVariantDimensionsInvalid
		}
		result.HeightCm = input.HeightCm
	}

	if input.WidthCm != nil {
		if *input.WidthCm < 0 {
			return product.ErrVariantDimensionsInvalid
		}
		result.WidthCm = input.WidthCm
	}

	if input.DepthCm != nil {
		if *input.DepthCm < 0 {
			return product.ErrVariantDimensionsInvalid
		}
		result.DepthCm = input.DepthCm
	}

	if input.Announced != nil {
		result.Announced = *input.Announced
	}

	if input.IsDefault != nil {
		result.IsDefault = *input.IsDefault
	}

	return nil
}

func (uc *updateProductUseCase) updateVariantCategory(result *product.Variant, input *product.UpdateVariantInput) error {
	if input.CategoryID == nil {
		return nil
	}

	catID := strings.TrimSpace(*input.CategoryID)
	if catID == "" {
		return product.ErrVariantCategoryRequired
	}

	exists, err := uc.categoryRepo.Exists(catID)
	if err != nil {
		return err
	}
	if !exists {
		return product.ErrVariantCategoryNotFound
	}

	result.CategoryID = &catID
	return nil
}

func (uc *updateProductUseCase) updateVariantMedia(result *product.Variant, input *product.UpdateVariantInput) error {
	if len(input.MediaIDs) == 0 {
		return nil
	}

	for _, mediaID := range input.MediaIDs {
		exists, err := uc.repo.MediaExists(mediaID)
		if err != nil {
			return err
		}
		if !exists {
			return product.ErrMediaNotFound
		}
	}

	result.MediaIDs = input.MediaIDs
	return nil
}

func (uc *updateProductUseCase) updateVariantOptions(result *product.Variant, input *product.UpdateVariantInput) error {
	if len(input.Options) == 0 {
		return nil
	}

	validOptions := make([]product.VariantOption, 0, len(input.Options))

	for _, opt := range input.Options {
		validatedOpt, err := uc.validateAndResolveOption(opt)
		if err != nil {
			return err
		}
		if validatedOpt != nil {
			validOptions = append(validOptions, *validatedOpt)
		}
	}

	if len(validOptions) > 0 {
		result.Options = validOptions
	}

	return nil
}

func (uc *updateProductUseCase) validateAndResolveOption(opt product.VariantOption) (*product.VariantOption, error) {
	result := opt

	if opt.OptionType != "" && opt.OptionValue != "" {
		optValueID, err := uc.repo.FindOrCreateOptionValue(opt.OptionType, opt.OptionValue)
		if err != nil {
			return nil, err
		}
		result.OptionValueID = optValueID
		return &result, nil
	}

	if opt.OptionValueID != "" {
		exists, err := uc.repo.OptionValueExists(opt.OptionValueID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, nil
		}
		return &result, nil
	}

	return nil, nil
}
