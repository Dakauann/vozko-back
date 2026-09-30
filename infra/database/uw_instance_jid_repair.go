package database

import "gorm.io/gorm"

const uwLiveInstanceJIDIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_live_jid
				ON unofficial_whatsapp_instances (jid)
				WHERE jid <> '' AND deleted_at IS NULL AND status = 'CONNECTED'`

func bareInstanceJIDs(tx *gorm.DB) error {
	if err := tx.Exec(`DROP INDEX IF EXISTS ux_uw_instance_jid`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE unofficial_whatsapp_instances AS i
		   SET status = 'DISCONNECTED',
		       status_reason = 'número reconectado em outra instância'
		  FROM (
		       SELECT id, ROW_NUMBER() OVER (
		              PARTITION BY regexp_replace(jid, ':[0-9]+@', '@')
		              ORDER BY created_at DESC, id
		       ) AS rank
		         FROM unofficial_whatsapp_instances
		        WHERE status = 'CONNECTED' AND jid <> '' AND deleted_at IS NULL
		  ) AS ranked
		 WHERE ranked.id = i.id AND ranked.rank > 1
	`).Error; err != nil {
		return err
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_instances
		   SET jid = regexp_replace(jid, ':[0-9]+@', '@')
		 WHERE jid ~ ':[0-9]+@'
	`).Error
}
