package property

import "vozko/domain/shared"

type PropertyRepository interface {
	Create(property *Property) error
	Update(propertyId string, property *Property) error
	Delete(propertyId string) error
	FindByID(propertyID string) (*Property, error)
	List(input ListPropertiesInput) (*shared.PaginatedResult[*Property], error)
	FindByIDs(propertyIDs []string) ([]*Property, error)
	Search(input SearchPropertiesInput) (*shared.PaginatedResult[*Property], error)
}
