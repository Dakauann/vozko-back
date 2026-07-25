package property_usecase

import (
	"vozko/domain/property"
)

type deletePropertyUseCase struct {
	repo property.PropertyRepository
}

func NewDeletePropertyUseCase(repo property.PropertyRepository) property.DeletePropertyUseCase {
	return &deletePropertyUseCase{repo: repo}
}

func (uc *deletePropertyUseCase) Execute(propertyID string) error {
	return uc.repo.Delete(propertyID)
}
