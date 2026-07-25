package address_repository

import (
	"vozko/domain/address"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) address.AddressRepository {
	return &repository{db: db}
}

func (r *repository) Create(addr *address.Address) error {
	dbAddress := &schema.Address{
		UserID:     addr.UserID,
		Name:       addr.Name,
		Street:     addr.Street,
		Number:     addr.Number,
		Complement: addr.Complement,
		District:   addr.District,
		City:       addr.City,
		State:      addr.State,
		ZipCode:    addr.ZipCode,
		IsDefault:  addr.IsDefault,
	}

	result := r.db.Create(dbAddress)
	if result.Error != nil {
		return result.Error
	}

	addr.ID = dbAddress.ID
	return nil
}

func (r *repository) GetByID(userID string, addressID string) (*address.Address, error) {
	var dbAddress schema.Address
	if err := r.db.Where("id = ? AND user_id = ?", addressID, userID).First(&dbAddress).Error; err != nil {
		return nil, err
	}
	return r.mapToAddress(&dbAddress), nil
}

func (r *repository) GetAllByUserID(userID string) ([]*address.Address, error) {
	var dbAddresses []schema.Address
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&dbAddresses).Error; err != nil {
		return nil, err
	}

	addresses := make([]*address.Address, len(dbAddresses))
	for i, dbAddress := range dbAddresses {
		addresses[i] = r.mapToAddress(&dbAddress)
	}
	return addresses, nil
}

func (r *repository) Update(addr *address.Address) error {
	return r.db.Model(&schema.Address{}).
		Where("id = ? AND user_id = ?", addr.ID, addr.UserID).
		Updates(schema.Address{
			Name:       addr.Name,
			Street:     addr.Street,
			Number:     addr.Number,
			Complement: addr.Complement,
			District:   addr.District,
			City:       addr.City,
			State:      addr.State,
			ZipCode:    addr.ZipCode,
			IsDefault:  addr.IsDefault,
		}).Error
}

func (r *repository) Delete(userID string, addressID string) error {
	return r.db.Where("id = ? AND user_id = ?", addressID, userID).
		Delete(&schema.Address{}).Error
}

func (r *repository) CountByUserID(userID string) (int, error) {
	var count int64
	if err := r.db.Model(&schema.Address{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *repository) UpdateDefaultStatus(userID string, addressID string, isDefault bool) error {
	if isDefault {
		if err := r.db.Model(&schema.Address{}).
			Where("user_id = ?", userID).
			Update("is_default", false).Error; err != nil {
			return err
		}
	}
	return r.db.Model(&schema.Address{}).
		Where("id = ? AND user_id = ?", addressID, userID).
		Update("is_default", isDefault).Error
}

func (r *repository) mapToAddress(dbAddress *schema.Address) *address.Address {
	return &address.Address{
		ID:         dbAddress.ID,
		UserID:     dbAddress.UserID,
		Name:       dbAddress.Name,
		Street:     dbAddress.Street,
		Number:     dbAddress.Number,
		Complement: dbAddress.Complement,
		District:   dbAddress.District,
		City:       dbAddress.City,
		State:      dbAddress.State,
		ZipCode:    dbAddress.ZipCode,
		IsDefault:  dbAddress.IsDefault,
		CreatedAt:  dbAddress.CreatedAt,
		UpdatedAt:  dbAddress.UpdatedAt,
	}
}
