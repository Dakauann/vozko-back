package address_usecase

import (
	"time"

	"vozko/domain/address"
)

type createAddressUseCase struct {
	repo address.AddressRepository
}

func NewCreateAddressUseCase(repo address.AddressRepository) address.CreateAddressUseCase {
	return &createAddressUseCase{repo: repo}
}

func (uc *createAddressUseCase) Execute(userID string, addr *address.Address) (*address.Address, error) {
	count, err := uc.repo.CountByUserID(userID)
	if err != nil {
		return nil, err
	}
	if count >= address.MaxAddressesPerUser {
		return nil, address.ErrMaxAddressesReached
	}

	if addr.IsDefault {
		err = uc.repo.UpdateDefaultStatus(userID, "", false)
		if err != nil {
			return nil, err
		}
	}

	addr.UserID = userID
	addr.CreatedAt = time.Now()
	addr.UpdatedAt = time.Now()

	err = uc.repo.Create(addr)
	if err != nil {
		return nil, err
	}

	return uc.repo.GetByID(userID, addr.ID)
}
