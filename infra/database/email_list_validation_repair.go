package database

import (
	"fmt"

	"gorm.io/gorm"
)

const (
	retiredEmailChecksTable      = "email_address_checks"
	retiredEmailCampaignsTable   = "email_campaigns"
	retiredEmailValidationColumn = "validation"
)

func dropEmailListValidation(tx *gorm.DB) error {
	if err := tx.Exec("DROP TABLE IF EXISTS " + retiredEmailChecksTable).Error; err != nil {
		return fmt.Errorf("%s: %w", retiredEmailChecksTable, err)
	}
	if !tx.Migrator().HasTable(retiredEmailCampaignsTable) || !tx.Migrator().HasColumn(retiredEmailCampaignsTable, retiredEmailValidationColumn) {
		return nil
	}
	if err := tx.Exec("ALTER TABLE " + retiredEmailCampaignsTable + " DROP COLUMN " + retiredEmailValidationColumn).Error; err != nil {
		return fmt.Errorf("%s.%s: %w", retiredEmailCampaignsTable, retiredEmailValidationColumn, err)
	}
	return nil
}
