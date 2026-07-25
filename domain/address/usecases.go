package address

type CreateAddressUseCase interface {
	Execute(userID string, address *Address) (*Address, error)
}

type GetAddressesUseCase interface {
	Execute(userID string) ([]*Address, error)
}

type UpdateAddressUseCase interface {
	Execute(userID string, addressID string, address *Address) (*Address, error)
}

type DeleteAddressUseCase interface {
	Execute(userID string, addressID string) error
}
