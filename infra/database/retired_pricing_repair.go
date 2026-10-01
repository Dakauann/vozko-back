package database

import "gorm.io/gorm"

func dropRetiredSIPTrunkPrices(tx *gorm.DB) error {
	return tx.Exec(`
		DELETE FROM plan_pricing_items
		 WHERE category = 'telephony'
		   AND service = 'sip_trunk'
	`).Error
}
