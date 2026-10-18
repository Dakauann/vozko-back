package whatsapp_campaign_wave_repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	campaigndomain "vozko/domain/campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

var (
	_ sc.PolicyRepository = (*Repository)(nil)
	_ sc.StateRepository  = (*Repository)(nil)
	_ sc.WaveRepository   = (*Repository)(nil)
)

func (r *Repository) FindPolicy(campaignID string) (*sc.Policy, error) {
	var rows []schema.WhatsAppCampaignWavePolicy
	if err := r.db.Where("campaign_id = ?", campaignID).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read wave policy: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var policy sc.Policy
	if err := json.Unmarshal(rows[0].Policy, &policy); err != nil {
		return nil, fmt.Errorf("decode wave policy of %s: %w", campaignID, err)
	}
	return &policy, nil
}

func (r *Repository) SavePolicy(campaignID string, policy sc.Policy) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode wave policy: %w", err)
	}
	row := schema.WhatsAppCampaignWavePolicy{CampaignID: campaignID, Policy: datatypes.JSON(raw)}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"policy", "updated_at"}),
	}).Create(&row).Error
}

func (r *Repository) DeletePolicy(campaignID string) error {
	return r.db.Where("campaign_id = ?", campaignID).Delete(&schema.WhatsAppCampaignWavePolicy{}).Error
}

func (r *Repository) FindState(campaignID string) (sc.State, error) {
	var rows []schema.WhatsAppCampaignWaveState
	if err := r.db.Where("campaign_id = ?", campaignID).Limit(1).Find(&rows).Error; err != nil {
		return sc.State{}, fmt.Errorf("read wave state: %w", err)
	}
	state := sc.State{CampaignID: campaignID}
	if len(rows) == 0 {
		return state, nil
	}
	row := rows[0]
	state.ResumedAfterHold = row.ResumedAfterHold
	state.UnreadSince = fromNullable(row.UnreadSince)
	state.DueAt = fromNullable(row.DueAt)
	if err := decodeOptional(row.Hold, &state.Hold); err != nil {
		return sc.State{}, err
	}
	if err := decodeOptional(row.Waiting, &state.Waiting); err != nil {
		return sc.State{}, err
	}
	return state, nil
}

func (r *Repository) SaveState(state sc.State) error {
	hold, err := encodeOptional(state.Hold)
	if err != nil {
		return err
	}
	waiting, err := encodeOptional(state.Waiting)
	if err != nil {
		return err
	}
	row := schema.WhatsAppCampaignWaveState{
		CampaignID:       state.CampaignID,
		Hold:             hold,
		Waiting:          waiting,
		ResumedAfterHold: state.ResumedAfterHold,
		UnreadSince:      toNullable(state.UnreadSince),
		DueAt:            toNullable(state.DueAt),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hold", "waiting", "resumed_after_hold", "unread_since", "due_at", "updated_at"}),
	}).Create(&row).Error
}

func (r *Repository) DueCampaigns(now time.Time, limit int) ([]string, error) {
	var ids []string
	err := r.db.Raw(`
		SELECT s.campaign_id::text FROM whatsapp_campaign_wave_states s
		JOIN whatsapp_campaign_wave_policies p ON p.campaign_id = s.campaign_id
		JOIN whatsapp_campaigns c ON c.id = s.campaign_id
		WHERE c.status = ? AND c.deleted_at IS NULL AND s.due_at IS NOT NULL AND s.due_at <= ?
		ORDER BY s.due_at
		LIMIT ?`,
		string(campaigndomain.StatusRunning), now.UTC(), limit,
	).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("list due smart campaigns: %w", err)
	}
	return ids, nil
}

func (r *Repository) RunningUsing(templateID, phoneID string) ([]string, error) {
	query := r.db.Table("whatsapp_campaigns c").
		Select("c.id::text").
		Joins("JOIN whatsapp_campaign_wave_policies p ON p.campaign_id = c.id").
		Where("c.status = ? AND c.deleted_at IS NULL", string(campaigndomain.StatusRunning))
	switch {
	case templateID != "" && phoneID != "":
		query = query.Where("c.template_id = ? OR c.business_phone_id = ?", templateID, phoneID)
	case templateID != "":
		query = query.Where("c.template_id = ?", templateID)
	case phoneID != "":
		query = query.Where("c.business_phone_id = ?", phoneID)
	default:
		return nil, nil
	}
	var ids []string
	if err := query.Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("list running smart campaigns: %w", err)
	}
	return ids, nil
}

func (r *Repository) CurrentWave(campaignID string) (*sc.Wave, error) {
	var rows []schema.WhatsAppCampaignWave
	if err := r.db.Where("campaign_id = ?", campaignID).Order("number DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read current wave: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	w, err := toDomainWave(rows[0])
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repository) ListWaves(campaignID string) ([]sc.Wave, error) {
	var rows []schema.WhatsAppCampaignWave
	if err := r.db.Where("campaign_id = ?", campaignID).Order("number").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list waves: %w", err)
	}
	waves := make([]sc.Wave, 0, len(rows))
	for _, row := range rows {
		w, err := toDomainWave(row)
		if err != nil {
			return nil, err
		}
		waves = append(waves, w)
	}
	return waves, nil
}

func (r *Repository) ClaimWave(campaignID string, number, size int, at time.Time) (sc.Wave, error) {
	var released sc.Wave
	err := r.db.Transaction(func(tx *gorm.DB) error {
		claim := tx.Exec(`
			UPDATE whatsapp_campaign_entries SET wave_number = ? WHERE id IN (
				SELECT id FROM whatsapp_campaign_entries
				WHERE campaign_id = ? AND status = ? AND wave_number = 0 AND deleted_at IS NULL
				ORDER BY created_at, id
				LIMIT ?
				FOR UPDATE SKIP LOCKED)`,
			number, campaignID, string(wce.SendStatusPending), size)
		if claim.Error != nil {
			return fmt.Errorf("claim wave contacts: %w", claim.Error)
		}
		if claim.RowsAffected == 0 {
			return sc.ErrNothingToClaim
		}
		released = sc.Release(campaignID, number, int(claim.RowsAffected), at)
		row, err := toSchemaWave(released)
		if err != nil {
			return err
		}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if insert.Error != nil {
			return fmt.Errorf("record wave: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			return sc.ErrAlreadyReleased
		}
		return nil
	})
	return released, err
}

func (r *Repository) SaveWave(w sc.Wave) error {
	row, err := toSchemaWave(w)
	if err != nil {
		return err
	}
	return r.db.Model(&schema.WhatsAppCampaignWave{}).
		Where("campaign_id = ? AND number = ?", w.CampaignID, w.Number).
		Select("size", "state", "drained_at", "observe_until", "decided_at", "outcome", "decision").
		Updates(&row).Error
}

func (r *Repository) ReturnToPool(campaignID string, number int) (int, error) {
	result := r.db.Exec(`
		UPDATE whatsapp_campaign_entries SET wave_number = 0
		WHERE campaign_id = ? AND wave_number = ? AND status = ? AND deleted_at IS NULL`,
		campaignID, number, string(wce.SendStatusPending))
	return int(result.RowsAffected), result.Error
}

func (r *Repository) ReleasedSince(campaignID string, since time.Time) (int, error) {
	var count int64
	err := r.db.Model(&schema.WhatsAppCampaignWave{}).Where("campaign_id = ? AND released_at >= ?", campaignID, since.UTC()).Count(&count).Error
	return int(count), err
}

func (r *Repository) PendingInWave(campaignID string, number int) (int, error) {
	var count int64
	err := r.db.Table("whatsapp_campaign_entries").
		Where("campaign_id = ? AND wave_number = ? AND status = ? AND deleted_at IS NULL", campaignID, number, string(wce.SendStatusPending)).
		Count(&count).Error
	return int(count), err
}

func (r *Repository) WaveOutcome(campaignID string, number int) (sc.Outcome, error) {
	var rows []struct {
		Status    string
		ErrorCode int
		Total     int
	}
	err := r.db.Raw(`
		SELECT status, error_code, COUNT(*) AS total FROM whatsapp_campaign_entries
		WHERE campaign_id = ? AND wave_number = ? AND deleted_at IS NULL
		GROUP BY status, error_code`,
		campaignID, number,
	).Scan(&rows).Error
	if err != nil {
		return sc.Outcome{}, fmt.Errorf("aggregate wave outcome: %w", err)
	}
	outcome := sc.Outcome{Failures: map[int]int{}}
	for _, row := range rows {
		switch wce.SendStatus(row.Status) {
		case wce.SendStatusPending:
			continue
		case wce.SendStatusFailed:
			outcome.Failures[row.ErrorCode] += row.Total
		case wce.SendStatusDelivered:
			outcome.Delivered += row.Total
		case wce.SendStatusRead:
			outcome.Delivered += row.Total
			outcome.Read += row.Total
		}
		outcome.Processed += row.Total
	}
	return outcome, nil
}

func (r *Repository) WaveRecipients(campaignID string, number int) ([]sc.Recipient, error) {
	var rows []struct {
		ID     string
		Number string
	}
	err := r.db.Raw(`
		SELECT e.id::text AS id, COALESCE(l.number, '') AS number FROM whatsapp_campaign_entries e
		JOIN leads l ON l.id = e.lead_id
		WHERE e.campaign_id = ? AND e.wave_number = ? AND e.status = ? AND e.deleted_at IS NULL
		ORDER BY e.created_at, e.id`,
		campaignID, number, string(wce.SendStatusPending),
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list wave recipients: %w", err)
	}
	recipients := make([]sc.Recipient, 0, len(rows))
	for _, row := range rows {
		recipients = append(recipients, sc.Recipient{EntryID: row.ID, PhoneNumber: row.Number})
	}
	return recipients, nil
}

func toSchemaWave(w sc.Wave) (schema.WhatsAppCampaignWave, error) {
	outcome, err := json.Marshal(w.Outcome)
	if err != nil {
		return schema.WhatsAppCampaignWave{}, fmt.Errorf("encode wave outcome: %w", err)
	}
	decision, err := json.Marshal(w.Decision)
	if err != nil {
		return schema.WhatsAppCampaignWave{}, fmt.Errorf("encode wave decision: %w", err)
	}
	return schema.WhatsAppCampaignWave{
		CampaignID:   w.CampaignID,
		Number:       w.Number,
		Size:         w.Size,
		State:        string(w.State),
		ReleasedAt:   w.ReleasedAt.UTC(),
		DrainedAt:    toNullable(w.DrainedAt),
		ObserveUntil: toNullable(w.ObserveUntil),
		DecidedAt:    toNullable(w.DecidedAt),
		Outcome:      datatypes.JSON(outcome),
		Decision:     datatypes.JSON(decision),
	}, nil
}

func toDomainWave(row schema.WhatsAppCampaignWave) (sc.Wave, error) {
	w := sc.Wave{
		CampaignID:   row.CampaignID,
		Number:       row.Number,
		Size:         row.Size,
		State:        sc.WaveState(row.State),
		ReleasedAt:   row.ReleasedAt,
		DrainedAt:    fromNullable(row.DrainedAt),
		ObserveUntil: fromNullable(row.ObserveUntil),
		DecidedAt:    fromNullable(row.DecidedAt),
	}
	if len(row.Outcome) > 0 {
		if err := json.Unmarshal(row.Outcome, &w.Outcome); err != nil {
			return sc.Wave{}, fmt.Errorf("decode wave outcome: %w", err)
		}
	}
	if len(row.Decision) > 0 {
		if err := json.Unmarshal(row.Decision, &w.Decision); err != nil {
			return sc.Wave{}, fmt.Errorf("decode wave decision: %w", err)
		}
	}
	return w, nil
}

func encodeOptional[T any](value *T) (datatypes.JSON, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode wave state: %w", err)
	}
	return datatypes.JSON(raw), nil
}

func decodeOptional[T any](raw datatypes.JSON, target **T) error {
	if len(raw) == 0 || string(raw) == "null" {
		*target = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.Join(errors.New("decode wave state"), err)
	}
	*target = &value
	return nil
}

func toNullable(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func fromNullable(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
