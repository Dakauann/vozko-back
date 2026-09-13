package audience_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
)

// The alert rule store.
//
// One method here carries the weight: ClaimFire. Everything else is ordinary
// CRUD, and the correctness argument for the whole feature is that a firing is
// decided by a single conditional UPDATE, the way ClaimForDispatch decides a
// scheduled message and ClaimByIDs decides a comment batch.

type alertRuleRepository struct{ db *gorm.DB }

func NewAlertRuleRepository(db *gorm.DB) ca.AlertRuleRepository {
	return &alertRuleRepository{db: db}
}

func (r *alertRuleRepository) Create(ctx context.Context, rule *ca.AlertRule) error {
	row := alertRuleFromDomain(rule)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	rule.ID = row.ID
	rule.CreatedAt, rule.UpdatedAt = row.CreatedAt, row.UpdatedAt
	return nil
}

// Update writes the CONFIGURATION only.
//
// The firing history is deliberately absent from the column list: it is owned
// by ClaimFire, and an operator saving a rule at the wrong moment must not
// reset a cooldown or a daily tally and let the alert fire again immediately.
func (r *alertRuleRepository) Update(ctx context.Context, rule *ca.AlertRule) error {
	res := r.db.WithContext(ctx).Model(&schema.AudienceAlertRule{}).
		Where("workspace_id = ? AND id = ?", rule.WorkspaceID, rule.ID).
		Updates(map[string]any{
			"name":              rule.Name,
			"enabled":           rule.Enabled,
			"metric":            string(rule.Metric),
			"threshold":         rule.Threshold,
			"window_minutes":    rule.WindowMinutes,
			"min_messages":      rule.MinMessages,
			"channel":           string(rule.Channel),
			"recipient":         rule.Recipient,
			"business_phone_id": nullableUUID(rule.BusinessPhoneID),
			"template_id":       nullableUUID(rule.TemplateID),
			"instance_id":       nullableUUID(rule.InstanceID),
			"brief":             rule.Brief,
			"cooldown_minutes":  rule.CooldownMinutes,
			"max_per_day":       rule.MaxPerDay,
			"updated_at":        time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ca.ErrNotFound
	}
	return nil
}

func (r *alertRuleRepository) Delete(ctx context.Context, workspaceID, id string) error {
	res := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Delete(&schema.AudienceAlertRule{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ca.ErrNotFound
	}
	return nil
}

func (r *alertRuleRepository) FindByID(ctx context.Context, workspaceID, id string) (*ca.AlertRule, error) {
	var row schema.AudienceAlertRule
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ca.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return alertRuleToDomain(&row), nil
}

func (r *alertRuleRepository) ListByAccount(ctx context.Context, workspaceID string, source ca.Source, accountID string) ([]*ca.AlertRule, error) {
	q := r.db.WithContext(ctx).Model(&schema.AudienceAlertRule{}).
		Where("workspace_id = ?", workspaceID)
	if source != "" {
		q = q.Where("source = ?", string(source))
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	var rows []schema.AudienceAlertRule
	if err := q.Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return alertRulesToDomain(rows), nil
}

// ListArmed is on the hot path: the engine calls it after every batch. It is
// served by idx_ca_alert_armed on (source, account_id, enabled), so an account
// with no rules costs an index probe rather than a scan.
func (r *alertRuleRepository) ListArmed(ctx context.Context, source ca.Source, accountID string) ([]*ca.AlertRule, error) {
	var rows []schema.AudienceAlertRule
	// The channel's own rules, plus the ones that watch every channel. A rule
	// with no source is the wildcard: watching four channels used to mean four
	// rules, each with its own cooldown and daily cap, so one incident spanning
	// two of them sent two messages.
	//
	// Still one index probe: idx_ca_alert_armed leads on source, and an IN of
	// two values is two probes rather than a scan.
	err := r.db.WithContext(ctx).
		Where("source IN ? AND account_id = ? AND enabled = true",
			[]string{string(source), ""}, accountID).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return alertRulesToDomain(rows), nil
}

// ClaimFire decides, atomically, whether THIS caller sends.
//
// One statement, guarded on everything that could forbid the firing, so two
// replicas evaluating the same batch cannot both send. A read-then-write would
// not do: both would observe a quiet rule and both would proceed, which is a
// duplicate WhatsApp message to a real person.
//
// The guards, all re-checked here rather than trusted from the domain:
//
//   - enabled, because the rule may have been switched off since it was listed.
//   - the cooldown, computed against the row's OWN cooldown_minutes so a rule
//     edited between listing and claiming is judged by its current setting.
//   - the daily cap, against today's tally, with the day rolled over in the
//     same expression that increments it.
//
// The cooldown is expressed as `cooldown_minutes * INTERVAL '1 minute'` rather
// than make_interval: GORM maps a Go int to bigint, and make_interval's named
// arguments are declared int, so the obvious spelling does not resolve.
func (r *alertRuleRepository) ClaimFire(ctx context.Context, workspaceID, id string, now time.Time) (bool, error) {
	day := now.UTC().Format("2006-01-02")
	res := r.db.WithContext(ctx).Exec(`
		UPDATE audience_alert_rules
		   SET last_fired_at = ?,
		       fired_today   = CASE WHEN fired_day = ? THEN fired_today + 1 ELSE 1 END,
		       fired_day     = ?,
		       last_error    = '',
		       updated_at    = ?
		 WHERE workspace_id = ?
		   AND id = ?
		   AND enabled = true
		   AND (last_fired_at IS NULL
		        OR last_fired_at + (cooldown_minutes * INTERVAL '1 minute') <= ?)
		   AND (fired_day <> ? OR fired_today < max_per_day)`,
		now, day, day, now, workspaceID, id, now, day)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// RecordFailure stores why a send failed, without touching the claim. The
// cooldown that the claim just started IS the retry interval.
func (r *alertRuleRepository) RecordFailure(ctx context.Context, workspaceID, id, message string, now time.Time) error {
	trimmed, _ := ca.TruncateRunes(message, 300)
	return r.db.WithContext(ctx).Model(&schema.AudienceAlertRule{}).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Updates(map[string]any{"last_error": trimmed, "updated_at": now}).Error
}

// ---- mapping ----

// nullableUUID keeps an empty optional id out of a uuid column, which would
// otherwise reject "" outright.
func nullableUUID(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func uuidOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func alertRulesToDomain(rows []schema.AudienceAlertRule) []*ca.AlertRule {
	out := make([]*ca.AlertRule, 0, len(rows))
	for i := range rows {
		out = append(out, alertRuleToDomain(&rows[i]))
	}
	return out
}

func alertRuleToDomain(row *schema.AudienceAlertRule) *ca.AlertRule {
	rule := &ca.AlertRule{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		Source:          ca.Source(row.Source),
		AccountID:       row.AccountID,
		Name:            row.Name,
		Enabled:         row.Enabled,
		CreatedByUserID: uuidOrEmpty(row.CreatedByUserID),
		Metric:          ca.AlertMetric(row.Metric),
		Threshold:       row.Threshold,
		WindowMinutes:   row.WindowMinutes,
		MinMessages:     row.MinMessages,
		Channel:         ca.AlertChannel(row.Channel),
		Recipient:       row.Recipient,
		BusinessPhoneID: uuidOrEmpty(row.BusinessPhoneID),
		TemplateID:      uuidOrEmpty(row.TemplateID),
		InstanceID:      uuidOrEmpty(row.InstanceID),
		Brief:           row.Brief,
		CooldownMinutes: row.CooldownMinutes,
		MaxPerDay:       row.MaxPerDay,
		LastFiredAt:     row.LastFiredAt,
		FiredToday:      row.FiredToday,
		FiredDay:        row.FiredDay,
		LastError:       row.LastError,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	// Normalized on the way out, so a row written by an older build reads back
	// under this build's floors rather than under the ones it was saved with.
	rule.Normalize()
	return rule
}

func alertRuleFromDomain(rule *ca.AlertRule) *schema.AudienceAlertRule {
	return &schema.AudienceAlertRule{
		ID:              rule.ID,
		WorkspaceID:     rule.WorkspaceID,
		Source:          string(rule.Source),
		AccountID:       rule.AccountID,
		Name:            rule.Name,
		Enabled:         rule.Enabled,
		CreatedByUserID: nullableUUID(rule.CreatedByUserID),
		Metric:          string(rule.Metric),
		Threshold:       rule.Threshold,
		WindowMinutes:   rule.WindowMinutes,
		MinMessages:     rule.MinMessages,
		Channel:         string(rule.Channel),
		Recipient:       rule.Recipient,
		BusinessPhoneID: nullableUUID(rule.BusinessPhoneID),
		TemplateID:      nullableUUID(rule.TemplateID),
		InstanceID:      nullableUUID(rule.InstanceID),
		Brief:           rule.Brief,
		CooldownMinutes: rule.CooldownMinutes,
		MaxPerDay:       rule.MaxPerDay,
		LastFiredAt:     rule.LastFiredAt,
		FiredToday:      rule.FiredToday,
		FiredDay:        rule.FiredDay,
		LastError:       rule.LastError,
	}
}
