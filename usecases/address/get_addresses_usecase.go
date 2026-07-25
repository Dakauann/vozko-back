package address_usecase

import "vozko/domain/address"

type getAddressesUseCase struct {
	repo address.AddressRepository
}

func NewGetAddressesUseCase(repo address.AddressRepository) address.GetAddressesUseCase {
	return &getAddressesUseCase{repo: repo}
}

func (uc *getAddressesUseCase) Execute(userID string) ([]*address.Address, error) {
	return uc.repo.GetAllByUserID(userID)
}
