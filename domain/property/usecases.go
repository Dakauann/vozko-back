package property

import "vozko/domain/shared"

type CreatePropertyUseCase interface {
	Execute(userID string, p *Property) error
}

type UpdatePropertyUseCase interface {
	Execute(userID string, propertyID string, p *Property) error
}

type GetPropertyUseCase interface {
	Execute(propertyID string) (*Property, error)
}

type ListPropertiesUseCase interface {
	Execute(input ListPropertiesInput) (*shared.PaginatedResult[*Property], error)
}

type SearchPropertiesUseCase interface {
	Execute(input SearchPropertiesInput) (*shared.PaginatedResult[*Property], error)
}

type DeletePropertyUseCase interface {
	Execute(propertyID string) error
}
