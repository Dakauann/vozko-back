package database

import (
	"fmt"

	"gorm.io/gorm"
)

const consumeUsedSchedulesSQL = `
	UPDATE %[1]s AS c
	   SET scheduled_start = '0001-01-01 00:00:00+00'
	 WHERE c.scheduled_start > '0001-01-01 00:00:00+00'
	   AND c.scheduled_start <= now()
	   AND (c.status <> 'STOPPED'
	        OR EXISTS (SELECT 1 FROM %[2]s e
	                    WHERE e.campaign_id = c.id AND e.status <> 'PENDING'))`

var scheduledCampaignTables = [][2]string{
	{"whatsapp_campaigns", "whatsapp_campaign_entries"},
	{"unofficial_whatsapp_campaigns", "unofficial_whatsapp_campaign_entries"},
}

func consumeUsedSchedules(tx *gorm.DB) error {
	for _, tables := range scheduledCampaignTables {
		if !tx.Migrator().HasColumn(tables[0], "scheduled_start") || !tx.Migrator().HasTable(tables[1]) {
			continue
		}
		if err := tx.Exec(fmt.Sprintf(consumeUsedSchedulesSQL, tables[0], tables[1])).Error; err != nil {
			return fmt.Errorf("%s: %w", tables[0], err)
		}
	}
	return nil
}
