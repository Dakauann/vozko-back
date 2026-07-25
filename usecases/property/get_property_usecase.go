package property_usecase

import (
	"vozko/domain/property"
)

type getPropertyUseCase struct {
	repo property.PropertyRepository
}

func NewGetPropertyUseCase(repo property.PropertyRepository) property.GetPropertyUseCase {
	return &getPropertyUseCase{repo: repo}
}

func (uc *getPropertyUseCase) Execute(propertyID string) (*property.Property, error) {
	return uc.repo.FindByID(propertyID)
}
