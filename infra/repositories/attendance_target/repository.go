package attendance_target_repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	at "vozko/domain/attendance_target"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) at.Repository {
	return &repository{db: db}
}

func (r *repository) GetByID(workspaceID, id string) (*at.Target, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" || id == "" {
		return nil, at.ErrNotFound
	}
	var record schema.AttendanceTarget
	err := r.db.Where("workspace_id = ? AND id = ?", workspaceID, id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, at.ErrNotFound
		}
		return nil, err
	}
	out := toDomain(record)
	return &out, nil
}

func (r *repository) ListForPeriod(workspaceID string, periodStart time.Time) ([]at.Target, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return []at.Target{}, nil
	}
	var records []schema.AttendanceTarget
	err := r.db.
		Where("workspace_id = ? AND period_start = ?", workspaceID, periodStart).
		Order("metric_key ASC, scope ASC, scope_id ASC").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(records), nil
}

func (r *repository) ListRange(workspaceID string, from, to time.Time) ([]at.Target, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return []at.Target{}, nil
	}
	var records []schema.AttendanceTarget
	err := r.db.
		Where("workspace_id = ? AND period_start >= ? AND period_start < ?", workspaceID, from, to).
		Order("period_start ASC, metric_key ASC, scope ASC, scope_id ASC").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(records), nil
}

func (r *repository) Upsert(target at.Target) (*at.Target, error) {
	record := schema.AttendanceTarget{
		ID:          target.ID,
		WorkspaceID: target.WorkspaceID,
		Scope:       string(target.Scope),
		ScopeID:     target.ScopeID,
		MetricKey:   target.MetricKey,
		PeriodStart: target.PeriodStart,
		Value:       target.Value,
		Currency:    target.Currency,
		CreatedBy:   target.CreatedBy,
	}

	err := r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "workspace_id"},
			{Name: "scope"},
			{Name: "scope_id"},
			{Name: "metric_key"},
			{Name: "period_start"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"value", "currency", "created_by", "updated_at"}),
	}).Create(&record).Error
	if err != nil {
		return nil, err
	}

	var stored schema.AttendanceTarget
	err = r.db.
		Where("workspace_id = ? AND scope = ? AND scope_id = ? AND metric_key = ? AND period_start = ?",
			target.WorkspaceID, string(target.Scope), target.ScopeID, target.MetricKey, target.PeriodStart).
		First(&stored).Error
	if err != nil {
		return nil, err
	}
	out := toDomain(stored)
	return &out, nil
}

func (r *repository) Delete(workspaceID, id string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" || id == "" {
		return at.ErrNotFound
	}
	result := r.db.
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Delete(&schema.AttendanceTarget{})
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return at.ErrNotFound
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return at.ErrNotFound
	}
	return nil
}

func toDomainSlice(records []schema.AttendanceTarget) []at.Target {
	out := make([]at.Target, 0, len(records))
	for _, record := range records {
		out = append(out, toDomain(record))
	}
	return out
}

func toDomain(record schema.AttendanceTarget) at.Target {
	return at.Target{
		ID:          record.ID,
		WorkspaceID: record.WorkspaceID,
		Scope:       at.Scope(record.Scope),
		ScopeID:     record.ScopeID,
		MetricKey:   record.MetricKey,
		PeriodStart: record.PeriodStart,
		Value:       record.Value,
		Currency:    record.Currency,
		CreatedBy:   record.CreatedBy,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
