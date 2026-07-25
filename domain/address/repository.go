package address

type AddressRepository interface {
	Create(address *Address) error
	GetByID(userID string, addressID string) (*Address, error)
	GetAllByUserID(userID string) ([]*Address, error)
	Update(address *Address) error
	Delete(userID string, addressID string) error
	CountByUserID(userID string) (int, error)
	UpdateDefaultStatus(userID string, addressID string, isDefault bool) error
}
