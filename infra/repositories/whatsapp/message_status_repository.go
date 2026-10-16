package whatsapp_repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/whatsapp/messagestatus"
	"vozko/infra/database/schema"
)

const (
	attributionPage = 5000
	rollUpHoursPage = 48
	purgePage       = 10000
)

const attributeFromCampaignsSQL = `
UPDATE whatsapp_message_status_events e
SET template_external_id = t.external_id, waba_id = t.waba_id, attributed_at = now()
FROM whatsapp_campaign_entries wce
JOIN whatsapp_campaigns c ON c.id = wce.campaign_id
JOIN whatsapp_templates t ON t.id = c.template_id
WHERE e.id > ? AND e.id <= ? AND e.attributed_at IS NULL AND wce.message_id = e.message_id`

const attributeFromDirectSendsSQL = `
UPDATE whatsapp_message_status_events e
SET template_external_id = t.external_id, waba_id = t.waba_id, attributed_at = now()
FROM whatsapp_template_sends s
JOIN whatsapp_templates t ON t.id = s.template_id
WHERE e.id > ? AND e.id <= ? AND e.attributed_at IS NULL AND s.provider_message_id = e.message_id`

const recountHourSQL = `
INSERT INTO whatsapp_template_hourly_stats (template_external_id, phone_number_id, hour, status, error_code, waba_id, messages, updated_at)
SELECT e.template_external_id, e.phone_number_id, date_trunc('hour', e.occurred_at), e.status, e.error_code, MAX(e.waba_id), COUNT(*), now()
FROM whatsapp_message_status_events e
WHERE e.template_external_id <> '' AND e.occurred_at >= ? AND e.occurred_at < ?
GROUP BY 1, 2, 3, 4, 5
ON CONFLICT (template_external_id, phone_number_id, hour, status, error_code)
DO UPDATE SET messages = EXCLUDED.messages, waba_id = EXCLUDED.waba_id, updated_at = EXCLUDED.updated_at`

type MessageStatusRepository struct {
	db *gorm.DB
}

func NewMessageStatusRepository(db *gorm.DB) *MessageStatusRepository {
	return &MessageStatusRepository{db: db}
}

var (
	_ messagestatus.Recorder      = (*MessageStatusRepository)(nil)
	_ messagestatus.Ledger        = (*MessageStatusRepository)(nil)
	_ messagestatus.SendingPhones = (*MessageStatusRepository)(nil)
)

func (r *MessageStatusRepository) Record(events ...messagestatus.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]schema.WhatsAppMessageStatusEvent, 0, len(events))
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}
		rows = append(rows, schema.WhatsAppMessageStatusEvent{
			MessageID: e.MessageID, Status: string(e.Status), PhoneNumberID: e.PhoneNumberID,
			ErrorCode: e.ErrorCode, Pricing: e.Pricing, OccurredAt: e.OccurredAt.UTC(),
		})
	}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("record whatsapp message status events: %w", err)
	}
	return nil
}

func (r *MessageStatusRepository) AttributePending(ctx context.Context, giveUpBefore time.Time) (int64, error) {
	var attributed int64
	var after int64
	for {
		if err := ctx.Err(); err != nil {
			return attributed, err
		}
		var last int64
		if err := r.db.WithContext(ctx).Raw(
			`SELECT COALESCE(MAX(id), 0) FROM (SELECT id FROM whatsapp_message_status_events WHERE attributed_at IS NULL AND id > ? ORDER BY id LIMIT ?) page`,
			after, attributionPage,
		).Scan(&last).Error; err != nil {
			return attributed, fmt.Errorf("page pending message status events: %w", err)
		}
		if last == 0 {
			break
		}
		for _, attributeSQL := range []string{attributeFromCampaignsSQL, attributeFromDirectSendsSQL} {
			res := r.db.WithContext(ctx).Exec(attributeSQL, after, last)
			if res.Error != nil {
				return attributed, fmt.Errorf("attribute message status events: %w", res.Error)
			}
			attributed += res.RowsAffected
		}
		after = last
	}
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE whatsapp_message_status_events SET attributed_at = now(), rolled_up_at = now()
		 WHERE attributed_at IS NULL AND recorded_at < ?`, giveUpBefore.UTC(),
	).Error; err != nil {
		return attributed, fmt.Errorf("set aside unattributed message status events: %w", err)
	}
	return attributed, nil
}

func (r *MessageStatusRepository) RollUpPending(ctx context.Context, recountAfter time.Time) (int64, error) {
	var hours int64
	for {
		if err := ctx.Err(); err != nil {
			return hours, err
		}
		var batch []time.Time
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Raw(
				`SELECT DISTINCT date_trunc('hour', occurred_at) AS hour FROM whatsapp_message_status_events
				 WHERE rolled_up_at IS NULL AND attributed_at IS NOT NULL ORDER BY hour LIMIT ?`, rollUpHoursPage,
			).Scan(&batch).Error; err != nil {
				return err
			}
			if len(batch) == 0 {
				return nil
			}
			if err := tx.Exec(
				`UPDATE whatsapp_message_status_events SET rolled_up_at = now()
				 WHERE rolled_up_at IS NULL AND attributed_at IS NOT NULL AND date_trunc('hour', occurred_at) IN ?`, batch,
			).Error; err != nil {
				return err
			}
			for _, hour := range batch {
				if !hour.Add(time.Hour).After(recountAfter) {
					continue
				}
				if err := tx.Exec(recountHourSQL, hour, hour.Add(time.Hour)).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return hours, fmt.Errorf("roll up message status events: %w", err)
		}
		if len(batch) == 0 {
			return hours, nil
		}
		hours += int64(len(batch))
	}
}

func (r *MessageStatusRepository) PhonesSendingSince(ctx context.Context, since time.Time) ([]string, error) {
	var phones []string
	if err := r.db.WithContext(ctx).Raw(
		`SELECT DISTINCT phone_number_id FROM whatsapp_template_hourly_stats WHERE hour >= ? AND status = ?`,
		since.UTC(), string(messagestatus.StatusSent),
	).Scan(&phones).Error; err != nil {
		return nil, fmt.Errorf("read phones sending templates: %w", err)
	}
	return phones, nil
}

func (r *MessageStatusRepository) PurgeRolledUpBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var purged int64
	for {
		if err := ctx.Err(); err != nil {
			return purged, err
		}
		res := r.db.WithContext(ctx).Exec(
			`DELETE FROM whatsapp_message_status_events WHERE id IN (
				SELECT id FROM whatsapp_message_status_events
				WHERE occurred_at < ? AND rolled_up_at IS NOT NULL
				LIMIT ?)`, cutoff.UTC(), purgePage,
		)
		if res.Error != nil {
			return purged, fmt.Errorf("purge message status events: %w", res.Error)
		}
		purged += res.RowsAffected
		if res.RowsAffected < purgePage {
			return purged, nil
		}
	}
}
