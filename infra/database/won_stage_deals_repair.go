package database

import (
	"log"

	"gorm.io/gorm"
)

func closeValuedDealsOnWonStages(tx *gorm.DB) error {
	res := tx.Exec(`
		WITH closed AS (
			UPDATE opportunities o
			   SET status = 'won',
			       close_date = COALESCE(o.close_date, o.updated_at),
			       closed_by_id = NULL,
			       closed_by_kind = 'system'
			  FROM stages s
			 WHERE s.id::text = o.stage_id::text
			   AND s.is_won
			   AND o.status <> 'won'
			   AND o.value_cents > 0
			   AND o.deleted_at IS NULL
			RETURNING o.id, o.workspace_id, o.stage_id, o.value_cents, o.currency
		)
		INSERT INTO opportunity_events
			(id, workspace_id, opportunity_id, type, actor_kind, to_stage_id, value_cents, currency, details, created_at)
		SELECT gen_random_uuid(), workspace_id, id, 'won', 'system', stage_id, value_cents, currency,
		       '{"repair":"won_stage_without_won_status"}'::jsonb, now()
		  FROM closed
	`)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("[data-repair] closed %d valued deal(s) left open on a won stage", res.RowsAffected)
	}
	return nil
}
