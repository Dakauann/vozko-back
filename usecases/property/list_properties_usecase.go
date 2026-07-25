package property_usecase

import (
	"vozko/domain/property"
	"vozko/domain/shared"
)

type listPropertiesUseCase struct {
	repo property.PropertyRepository
}

func NewListPropertiesUseCase(repo property.PropertyRepository) property.ListPropertiesUseCase {
	return &listPropertiesUseCase{repo: repo}
}

func (uc *listPropertiesUseCase) Execute(input property.ListPropertiesInput) (*shared.PaginatedResult[*property.Property], error) {
	return uc.repo.List(input)
}
