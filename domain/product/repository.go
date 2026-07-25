package product

import "vozko/domain/shared"

type ProductRepository interface {
	Create(product *Product) error
	Update(productId string, product *Product) error
	Delete(productId string) error
	FindByID(productID string) (*Product, error)
	List(input ListProductsInput) (*shared.PaginatedResult[*Product], error)
	FindByIDs(productIDs []string) ([]*Product, error)
	CreateOptionType(optionType *OptionType) error
	CreateOptionValue(optionValue *OptionValue) error
	ListOptionTypes() ([]*OptionType, error)
	ListOptionValues(optionTypeID string) ([]*OptionValue, error)
	OptionValueExists(optionValueID string) (bool, error)
	FindOrCreateOptionValue(optionType, optionValue string) (string, error)
	MediaExists(mediaID string) (bool, error)
	Search(input SearchProductsInput) (*shared.PaginatedResult[*Product], error)
}
