package product

import (
	"errors"
	"time"

	"vozko/domain/category"
	"vozko/domain/media"
)

var (
	ErrProductNameRequired              = errors.New("product name is required")
	ErrProductNameTooShort              = errors.New("product name must be at least 3 characters")
	ErrProductNameTooLong               = errors.New("product name must not exceed 255 characters")
	ErrProductDescriptionRequired       = errors.New("product description is required")
	ErrProductDescriptionTooShort       = errors.New("product description must be at least 10 characters")
	ErrProductVariantsRequired          = errors.New("at least one variant is required")
	ErrVariantSKURequired               = errors.New("variant SKU is required")
	ErrVariantSKUTooLong                = errors.New("variant SKU must not exceed 50 characters")
	ErrVariantSKUDuplicate              = errors.New("variant SKU already exists")
	ErrVariantRetailPriceInvalid        = errors.New("variant retail price must be greater than 0")
	ErrVariantWholesalePriceInvalid     = errors.New("variant wholesale price must be greater than 0")
	ErrVariantCostInvalid               = errors.New("variant cost cannot be negative")
	ErrVariantInventoryInvalid          = errors.New("variant inventory cannot be negative")
	ErrVariantWeightInvalid             = errors.New("variant weight cannot be negative")
	ErrVariantDimensionsInvalid         = errors.New("variant dimensions cannot be negative")
	ErrVariantVslDiscountInvalid        = errors.New("variant VSL discount must be greater than 0")
	ErrVariantMinQuantityInvalid        = errors.New("variant minimum quantity for wholesale must be greater than 0")
	ErrVariantOptionTypeRequired        = errors.New("variant option type is required")
	ErrVariantOptionValueRequired       = errors.New("variant option value is required")
	ErrVariantMediaRequired             = errors.New("at least one media is required for each variant")
	ErrVariantNameTooLong               = errors.New("variant name must not exceed 255 characters")
	ErrVariantNotFound                  = errors.New("variant not found")
	ErrMediaNotFound                    = errors.New("media not found")
	ErrProductNotFound                  = errors.New("product not found")
	ErrVariantStockAdjustmentInvalid    = errors.New("variant stock adjustment must be greater than 0")
	ErrVariantInventoryUpdateNotAllowed = errors.New("variant inventory cannot be updated via product update")
	ErrVariantCategoryRequired          = errors.New("variant category is required")
	ErrVariantCategoryNotFound          = errors.New("variant category not found")
	ErrVariantCategoryMustBeLeaf        = errors.New("variant category must be a leaf category (no subcategories)")
	ErrProductShopNotFound              = errors.New("Product shop not found")
	ErrProductShopUnauthorized          = errors.New("unauthorized to manage products for this shop")
)

type Product struct {
	ID                 string    `json:"id"`
	ShopID             int64     `json:"shopId"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Tags               []string  `json:"tags"`
	IsFromOfficialShop bool      `json:"is_from_official_shop"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Variants           []Variant `json:"variants"`
}

type OptionType struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Values []OptionValue `json:"values"`
}

type OptionValue struct {
	ID           string      `json:"id"`
	OptionTypeID string      `json:"optionTypeId"`
	Value        string      `json:"value"`
	OptionType   *OptionType `json:"-"`
}

type Variant struct {
	Name                    string             `json:"name"`
	ID                      string             `json:"id"`
	ProductID               string             `json:"productId"`
	SKU                     string             `json:"sku"`
	RetailPrice             *float64           `json:"retailPrice"`
	WholesalePrice          *float64           `json:"wholesalePrice"`
	Cost                    *float64           `json:"cost"`
	Inventory               int                `json:"inventory"`
	BaseInventory           int                `json:"baseInventory,omitempty"`
	LaunchedStock           int                `json:"launchedStock,omitempty"`
	ReservedStock           int                `json:"reservedStock,omitempty"`
	SoldStock               int                `json:"soldStock,omitempty"`
	Announced               bool               `json:"announced"`
	MinQuantityForWholesale *int               `json:"minQuantityForWholesale"`
	WeightKg                *float64           `json:"weight"`
	HeightCm                *float64           `json:"height"`
	WidthCm                 *float64           `json:"width"`
	DepthCm                 *float64           `json:"depth"`
	MediaIDs                []string           `json:"mediaIds"`
	Medias                  []media.Media      `json:"medias"`
	IsDefault               bool               `json:"isDefault"`
	CreatedAt               time.Time          `json:"createdAt"`
	UpdatedAt               time.Time          `json:"updatedAt"`
	Product                 *Product           `json:"-"`
	Options                 []VariantOption    `json:"options,omitempty"`
	CategoryID              *string            `json:"categoryId"`
	Category                *category.Category `json:"category"`
}

var (
	ErrVariantNameRequired = errors.New("variant name is required")
)

func (v Variant) PriceForQuantity(quantity int) float64 {
	wholesalePrice := float64(0)
	retailPrice := float64(0)
	minQty := 0

	if v.WholesalePrice != nil {
		wholesalePrice = *v.WholesalePrice
	}
	if v.RetailPrice != nil {
		retailPrice = *v.RetailPrice
	}
	if v.MinQuantityForWholesale != nil {
		minQty = *v.MinQuantityForWholesale
	}

	if minQty > 0 && quantity >= minQty {
		return wholesalePrice
	}
	return retailPrice
}

type VariantOption struct {
	ID            string `json:"id"`
	VariantID     string `json:"variantId"`
	OptionValueID string `json:"optionValueId"`
	OptionType    string `json:"optionType,omitempty"`
	OptionValue   string `json:"optionValue,omitempty"`
}

type UpdateVariantInput struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name,omitempty"`
	SKU                     string          `json:"sku,omitempty"`
	RetailPrice             *float64        `json:"retailPrice,omitempty"`
	WholesalePrice          *float64        `json:"wholesalePrice,omitempty"`
	Cost                    *float64        `json:"cost,omitempty"`
	Inventory               *int            `json:"inventory,omitempty"`
	MinQuantityForWholesale *int            `json:"minQuantityForWholesale,omitempty"`
	WeightKg                *float64        `json:"weight,omitempty"`
	HeightCm                *float64        `json:"height,omitempty"`
	WidthCm                 *float64        `json:"width,omitempty"`
	DepthCm                 *float64        `json:"depth,omitempty"`
	MediaIDs                []string        `json:"mediaIds,omitempty"`
	IsDefault               *bool           `json:"isDefault,omitempty"`
	Announced               *bool           `json:"announced,omitempty"`
	Options                 []VariantOption `json:"options,omitempty"`
	CategoryID              *string         `json:"categoryId,omitempty"`
}

type UpdateProductInput struct {
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Variants    []UpdateVariantInput `json:"variants,omitempty"`
}
