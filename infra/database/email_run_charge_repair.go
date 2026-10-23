package database

import (
	"fmt"

	"gorm.io/gorm"
)

var retiredEmailRunChargeColumns = []string{"charged_units", "unit_price_micros", "unit_cost_micros", "charged_total_micros"}

func dropEmailRunChargeColumns(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(retiredEmailCampaignsTable) {
		return nil
	}
	for _, column := range retiredEmailRunChargeColumns {
		if err := tx.Exec("ALTER TABLE " + retiredEmailCampaignsTable + " DROP COLUMN IF EXISTS " + column).Error; err != nil {
			return fmt.Errorf("%s.%s: %w", retiredEmailCampaignsTable, column, err)
		}
	}
	return nil
}
