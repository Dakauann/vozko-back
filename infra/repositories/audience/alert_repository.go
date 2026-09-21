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

func (r *alertRuleRepository) ListArmed(ctx context.Context, source ca.Source, accountID string) ([]*ca.AlertRule, error) {
	var rows []schema.AudienceAlertRule
	err := r.db.WithContext(ctx).
		Where("source IN ? AND account_id = ? AND enabled = true",
			[]string{string(source), ""}, accountID).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return alertRulesToDomain(rows), nil
}

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

func (r *alertRuleRepository) RecordFailure(ctx context.Context, workspaceID, id, message string, now time.Time) error {
	trimmed, _ := ca.TruncateRunes(message, 300)
	return r.db.WithContext(ctx).Model(&schema.AudienceAlertRule{}).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Updates(map[string]any{"last_error": trimmed, "updated_at": now}).Error
}

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
