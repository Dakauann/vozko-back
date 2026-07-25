package property_usecase

import (
	"vozko/domain/property"
	"vozko/domain/shared"
)

type searchPropertiesUseCase struct {
	repo property.PropertyRepository
}

func NewSearchPropertiesUseCase(repo property.PropertyRepository) property.SearchPropertiesUseCase {
	return &searchPropertiesUseCase{repo: repo}
}

func (uc *searchPropertiesUseCase) Execute(input property.SearchPropertiesInput) (*shared.PaginatedResult[*property.Property], error) {
	return uc.repo.Search(input)
}
