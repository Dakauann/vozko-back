package address_usecase

import (
	"time"

	"vozko/domain/address"
)

type updateAddressUseCase struct {
	repo address.AddressRepository
}

func NewUpdateAddressUseCase(repo address.AddressRepository) address.UpdateAddressUseCase {
	return &updateAddressUseCase{repo: repo}
}

func (uc *updateAddressUseCase) Execute(userID string, addressID string, addr *address.Address) (*address.Address, error) {
	existingAddress, err := uc.repo.GetByID(userID, addressID)
	if err != nil {
		return nil, address.ErrAddressNotFound
	}

	if addr.IsDefault && !existingAddress.IsDefault {
		err = uc.repo.UpdateDefaultStatus(userID, "", false)
		if err != nil {
			return nil, err
		}
	}

	addr.ID = addressID
	addr.UserID = userID
	addr.UpdatedAt = time.Now()

	err = uc.repo.Update(addr)
	if err != nil {
		return nil, err
	}

	return uc.repo.GetByID(userID, addressID)
}
