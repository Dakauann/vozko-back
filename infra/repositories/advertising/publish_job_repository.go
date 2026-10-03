package advertising_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type publishJobRepository struct {
	db *gorm.DB
}

func NewPublishJobRepository(db *gorm.DB) advertising.PublishJobRepository {
	return &publishJobRepository{db: db}
}

func (r *publishJobRepository) Create(ctx context.Context, job *advertising.PublishJob) error {
	if blank(job.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	record, err := toJobRecord(job)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	job.ID, job.CreatedAt, job.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *publishJobRepository) Find(ctx context.Context, workspaceID, id string) (*advertising.PublishJob, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdPublishJob
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrJobNotFound
		}
		return nil, err
	}
	return toJob(&record)
}

func (r *publishJobRepository) Save(ctx context.Context, job *advertising.PublishJob) error {
	if blank(job.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	record, err := toJobRecord(job)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.AdPublishJob{}).
		Where("id = ? AND workspace_id = ?", job.ID, job.WorkspaceID).
		Updates(map[string]any{
			"draft":         record.Draft,
			"status":        record.Status,
			"progress":      record.Progress,
			"fee":           record.Fee,
			"fee_micros":    record.FeeMicros,
			"error_code":    record.ErrorCode,
			"error_message": record.ErrorMessage,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrJobNotFound
	}
	return nil
}

func (r *publishJobRepository) Claim(ctx context.Context, id string, from, to advertising.JobStatus) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.AdPublishJob{}).
		Where("id = ? AND status = ?", id, string(from)).
		Updates(map[string]any{"status": string(to), "updated_at": gorm.Expr("NOW()")})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *publishJobRepository) ListByWorkspace(ctx context.Context, workspaceID string, limit int) ([]*advertising.PublishJob, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdPublishJob
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).
		Order("created_at DESC, id").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return toJobs(records)
}

func (r *publishJobRepository) ListStale(ctx context.Context, before time.Time, limit int) ([]*advertising.PublishJob, error) {
	var records []schema.AdPublishJob
	if err := r.db.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", []string{string(advertising.JobRunning), string(advertising.JobQueued)}, before).
		Order("updated_at, id").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return toJobs(records)
}

func toJobs(records []schema.AdPublishJob) ([]*advertising.PublishJob, error) {
	out := make([]*advertising.PublishJob, 0, len(records))
	for i := range records {
		job, err := toJob(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func toJobRecord(job *advertising.PublishJob) (*schema.AdPublishJob, error) {
	draft, err := encodeJSON(job.Draft)
	if err != nil {
		return nil, err
	}
	progress, err := encodeJSON(job.Progress)
	if err != nil {
		return nil, err
	}
	return &schema.AdPublishJob{
		ID:           job.ID,
		WorkspaceID:  job.WorkspaceID,
		AdAccountID:  job.AdAccountID,
		CreatedBy:    job.CreatedBy,
		Actor:        string(job.Actor),
		Draft:        draft,
		Status:       string(job.Status),
		Progress:     progress,
		Fee:          string(job.Fee),
		FeeMicros:    job.FeeMicros,
		ErrorCode:    job.ErrorCode,
		ErrorMessage: job.ErrorMessage,
	}, nil
}

func toJob(r *schema.AdPublishJob) (*advertising.PublishJob, error) {
	job := &advertising.PublishJob{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		AdAccountID:  r.AdAccountID,
		CreatedBy:    r.CreatedBy,
		Actor:        advertising.Actor(r.Actor),
		Status:       advertising.JobStatus(r.Status),
		Fee:          advertising.FeeState(r.Fee),
		FeeMicros:    r.FeeMicros,
		ErrorCode:    r.ErrorCode,
		ErrorMessage: r.ErrorMessage,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if err := decodeJSON(r.Draft, &job.Draft); err != nil {
		return nil, err
	}
	if err := decodeJSON(r.Progress, &job.Progress); err != nil {
		return nil, err
	}
	return job, nil
}
