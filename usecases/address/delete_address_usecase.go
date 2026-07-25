package address_usecase

import "vozko/domain/address"

type deleteAddressUseCase struct {
	repo address.AddressRepository
}

func NewDeleteAddressUseCase(repo address.AddressRepository) address.DeleteAddressUseCase {
	return &deleteAddressUseCase{repo: repo}
}

func (uc *deleteAddressUseCase) Execute(userID string, addressID string) error {
	return uc.repo.Delete(userID, addressID)
}
