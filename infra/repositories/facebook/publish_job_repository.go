package facebook_repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/database/schema"
)

type publishJobRepository struct {
	db *gorm.DB
}

func NewPublishJobRepository(db *gorm.DB) fbdomain.PublishJobRepository {
	return &publishJobRepository{db: db}
}

func (r *publishJobRepository) Create(ctx context.Context, job *fbdomain.PublishJob) error {
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

func (r *publishJobRepository) FindByID(ctx context.Context, id string) (*fbdomain.PublishJob, error) {
	var record schema.FacebookPublishJob
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrPublishJobNotFound
		}
		return nil, err
	}
	return toJobDomain(&record)
}

func (r *publishJobRepository) Save(ctx context.Context, job *fbdomain.PublishJob) error {
	record, err := toJobRecord(job)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.FacebookPublishJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"progress":      record.Progress,
		"status":        record.Status,
		"fb_object_id":  record.FBObjectID,
		"fb_post_id":    record.FBPostID,
		"error_code":    record.ErrorCode,
		"error_subcode": record.ErrorSubcode,
		"error_message": record.ErrorMessage,
		"ambiguous":     record.Ambiguous,
		"next_check_at": record.NextCheckAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrPublishJobNotFound
	}
	return nil
}

func (r *publishJobRepository) Claim(ctx context.Context, id string, from, to fbdomain.JobStatus) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.FacebookPublishJob{}).
		Where("id = ? AND status = ?", id, string(from)).
		Updates(map[string]any{"status": string(to), "attempts": gorm.Expr("attempts + 1")})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *publishJobRepository) ListByPage(ctx context.Context, pageID string, status fbdomain.JobStatus, limit int) ([]*fbdomain.PublishJob, error) {
	query := r.db.WithContext(ctx).Where("page_id = ?", pageID)
	if status != "" {
		query = query.Where("status = ?", string(status))
	}
	var records []schema.FacebookPublishJob
	if err := query.Order("created_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*fbdomain.PublishJob, 0, len(records))
	for i := range records {
		job, err := toJobDomain(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func (r *publishJobRepository) ReleaseStale(ctx context.Context, before time.Time, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Raw(`UPDATE facebook_publish_jobs SET status = 'QUEUED', updated_at = NOW()
		WHERE id IN (SELECT id FROM facebook_publish_jobs
			WHERE status IN ('QUEUED','UPLOADING') AND updated_at < ?
			ORDER BY updated_at LIMIT ? FOR UPDATE SKIP LOCKED)
		RETURNING id`, before, limit).Scan(&ids).Error
	return ids, err
}

func toJobRecord(job *fbdomain.PublishJob) (*schema.FacebookPublishJob, error) {
	request, err := json.Marshal(job.Request)
	if err != nil {
		return nil, err
	}
	progress, err := json.Marshal(job.Progress)
	if err != nil {
		return nil, err
	}
	return &schema.FacebookPublishJob{
		ID:           job.ID,
		WorkspaceID:  job.WorkspaceID,
		PageID:       job.PageID,
		RequestedBy:  job.RequestedBy,
		Kind:         string(job.Request.Kind),
		Request:      datatypes.JSON(request),
		Progress:     datatypes.JSON(progress),
		Status:       string(job.Status),
		FBObjectID:   job.FBObjectID,
		FBPostID:     job.FBPostID,
		ErrorCode:    job.ErrorCode,
		ErrorSubcode: job.ErrorSubcode,
		ErrorMessage: job.ErrorMessage,
		Ambiguous:    job.Ambiguous,
		Attempts:     job.Attempts,
		NextCheckAt:  job.NextCheckAt,
	}, nil
}

func toJobDomain(r *schema.FacebookPublishJob) (*fbdomain.PublishJob, error) {
	job := &fbdomain.PublishJob{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		PageID:       r.PageID,
		RequestedBy:  r.RequestedBy,
		Status:       fbdomain.JobStatus(r.Status),
		FBObjectID:   r.FBObjectID,
		FBPostID:     r.FBPostID,
		ErrorCode:    r.ErrorCode,
		ErrorSubcode: r.ErrorSubcode,
		ErrorMessage: r.ErrorMessage,
		Ambiguous:    r.Ambiguous,
		Attempts:     r.Attempts,
		NextCheckAt:  r.NextCheckAt,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if err := json.Unmarshal(r.Request, &job.Request); err != nil {
		return nil, err
	}
	if len(r.Progress) > 0 {
		if err := json.Unmarshal(r.Progress, &job.Progress); err != nil {
			return nil, err
		}
	}
	return job, nil
}

func (r *publishJobRepository) FindProcessingByVideoID(ctx context.Context, videoID string) (*fbdomain.PublishJob, error) {
	var record schema.FacebookPublishJob
	if err := r.db.WithContext(ctx).First(&record, "fb_object_id = ? AND status = ?", videoID, string(fbdomain.JobProcessing)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrPublishJobNotFound
		}
		return nil, err
	}
	return toJobDomain(&record)
}

func (r *publishJobRepository) ListDueProcessing(ctx context.Context, now time.Time, limit int) ([]*fbdomain.PublishJob, error) {
	var records []schema.FacebookPublishJob
	if err := r.db.WithContext(ctx).
		Where("status = ? AND next_check_at <= ?", string(fbdomain.JobProcessing), now).
		Order("next_check_at").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*fbdomain.PublishJob, 0, len(records))
	for i := range records {
		job, err := toJobDomain(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func (r *publishJobRepository) CountSince(ctx context.Context, pageID string, kind fbdomain.PublishKind, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&schema.FacebookPublishJob{}).
		Where("page_id = ? AND kind = ? AND created_at >= ? AND status <> ?", pageID, string(kind), since, string(fbdomain.JobFailed)).
		Count(&n).Error
	return n, err
}
