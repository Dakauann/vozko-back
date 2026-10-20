package database

import "gorm.io/gorm"

func AffectedOne(result *gorm.DB, notFound error) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return notFound
	}
	return nil
}
