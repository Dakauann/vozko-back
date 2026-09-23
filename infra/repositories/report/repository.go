package report_repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/report"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) report.Repository {
	return &repository{db: db}
}

func (r *repository) Create(job *report.Job) error {
	record := schema.ReportJob{
		ID:          job.ID,
		WorkspaceID: job.WorkspaceID,
		RequestedBy: job.RequestedBy,
		Kind:        string(job.Kind),
		Format:      string(job.Format),
		Locale:      job.Locale,
		Params:      datatypes.JSON(job.Params),
		Fingerprint: job.Fingerprint,
		Status:      string(job.Status),
		ExpiresAt:   job.ExpiresAt,
	}
	if record.RequestedBy == "" {
		if err := r.db.Omit("RequestedBy").Create(&record).Error; err != nil {
			return err
		}
	} else if err := r.db.Create(&record).Error; err != nil {
		return err
	}
	job.ID = record.ID
	job.CreatedAt = record.CreatedAt
	job.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *repository) GetByID(workspaceID, id string) (*report.Job, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" || id == "" {
		return nil, report.ErrNotFound
	}
	var record schema.ReportJob
	err := r.db.Where("id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, report.ErrNotFound
		}
		return nil, err
	}
	if record.WorkspaceID != workspaceID {
		return nil, report.ErrForbidden
	}
	out := toDomain(record)
	return &out, nil
}

func (r *repository) FindReusable(workspaceID, fingerprint string, since time.Time) (*report.Job, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || fingerprint == "" {
		return nil, report.ErrNotFound
	}
	var record schema.ReportJob
	err := r.db.
		Where("workspace_id = ? AND fingerprint = ?", workspaceID, fingerprint).
		Where("status IN ?", []string{string(report.StatusQueued), string(report.StatusRunning), string(report.StatusDone)}).
		Where("created_at >= ?", since).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		Order("created_at DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, report.ErrNotFound
		}
		return nil, err
	}
	out := toDomain(record)
	return &out, nil
}

func (r *repository) List(query report.ListQuery) (report.ListPage, error) {
	query.Normalize()
	if query.WorkspaceID == "" {
		return report.ListPage{Jobs: []report.Job{}}, nil
	}

	scope := r.db.Model(&schema.ReportJob{}).Where("workspace_id = ?", query.WorkspaceID)

	if len(query.Kinds) > 0 {
		kinds := make([]string, 0, len(query.Kinds))
		for _, kind := range query.Kinds {
			kinds = append(kinds, string(kind))
		}
		scope = scope.Where("kind IN ?", kinds)
	}
	if len(query.Statuses) > 0 {
		statuses := make([]string, 0, len(query.Statuses))
		for _, status := range query.Statuses {
			statuses = append(statuses, string(status))
		}
		scope = scope.Where("status IN ?", statuses)
	}
	if query.RequestedBy != "" {
		scope = scope.Where("requested_by = ?", query.RequestedBy)
	}
	if query.CreatedFrom != nil {
		scope = scope.Where("created_at >= ?", *query.CreatedFrom)
	}
	if query.CreatedTo != nil {
		scope = scope.Where("created_at <= ?", *query.CreatedTo)
	}

	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return report.ListPage{}, err
	}

	var records []schema.ReportJob
	err := scope.
		Order("created_at DESC").
		Limit(query.Limit).
		Offset(query.Offset).
		Find(&records).Error
	if err != nil {
		return report.ListPage{}, err
	}

	jobs := make([]report.Job, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, toDomain(record))
	}
	return report.ListPage{Jobs: jobs, Total: total}, nil
}

func (r *repository) MarkRunning(id string, at time.Time) error {
	result := r.db.Model(&schema.ReportJob{}).
		Where("id = ? AND status = ?", id, string(report.StatusQueued)).
		Updates(map[string]interface{}{
			"status":     string(report.StatusRunning),
			"started_at": at,
			"progress":   0,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return report.ErrNotFound
	}
	return nil
}

func (r *repository) MarkDone(
	id, objectKey, filename string,
	size, rows int64,
	at time.Time,
	expiresAt *time.Time,
) error {
	updates := map[string]interface{}{
		"status":       string(report.StatusDone),
		"object_key":   objectKey,
		"filename":     filename,
		"size_bytes":   size,
		"row_count":    rows,
		"progress":     100,
		"finished_at":  at,
		"failure_code": "",
	}
	if expiresAt != nil {
		updates["expires_at"] = *expiresAt
	}
	return r.db.Model(&schema.ReportJob{}).Where("id = ?", id).Updates(updates).Error
}

func (r *repository) MarkFailed(id string, failureCode report.FailureCode, at time.Time) error {
	return r.db.Model(&schema.ReportJob{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       string(report.StatusFailed),
		"failure_code": string(failureCode),
		"finished_at":  at,
	}).Error
}

func (r *repository) UpdateProgress(id string, percent int) error {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return r.db.Model(&schema.ReportJob{}).
		Where("id = ? AND status = ?", id, string(report.StatusRunning)).
		Update("progress", percent).Error
}

func (r *repository) ExpireBefore(now time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var ids []string
	err := r.db.Model(&schema.ReportJob{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", string(report.StatusDone), now).
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.Model(&schema.ReportJob{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"status":     string(report.StatusExpired),
			"object_key": "",
		})
	return result.RowsAffected, result.Error
}

func toDomain(record schema.ReportJob) report.Job {
	params := json.RawMessage(record.Params)
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	return report.Job{
		ID:          record.ID,
		WorkspaceID: record.WorkspaceID,
		RequestedBy: record.RequestedBy,
		Kind:        report.Kind(record.Kind),
		Format:      report.Format(record.Format),
		Locale:      record.Locale,
		Params:      params,
		Fingerprint: record.Fingerprint,
		Status:      report.Status(record.Status),
		Progress:    record.Progress,
		FailureCode: report.FailureCode(record.FailureCode),
		ObjectKey:   record.ObjectKey,
		Filename:    record.Filename,
		SizeBytes:   record.SizeBytes,
		RowCount:    record.RowCount,
		ExpiresAt:   record.ExpiresAt,
		StartedAt:   record.StartedAt,
		FinishedAt:  record.FinishedAt,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
