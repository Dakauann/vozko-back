package imagegen_repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/imagegen"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

var unfinished = []string{string(imagegen.StatusQueued), string(imagegen.StatusRunning)}

type jobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) imagegen.Repository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job *imagegen.Job) error {
	record := toRecord(job)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return imagegen.ErrDuplicateActiveJob
		}
		return err
	}
	job.ID, job.CreatedAt, job.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *jobRepository) Get(ctx context.Context, workspaceID, id string) (*imagegen.Job, error) {
	if !validID(id) || !validID(workspaceID) {
		return nil, imagegen.ErrJobNotFound
	}
	var record schema.ImageGenerationJob
	err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&record).Error
	return found(&record, err)
}

func (r *jobRepository) FindActive(ctx context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*imagegen.Job, error) {
	var record schema.ImageGenerationJob
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND requested_by = ? AND fingerprint = ? AND status IN ? AND created_at >= ?",
			workspaceID, requestedBy, fingerprint, unfinished, since).
		Order("created_at DESC").First(&record).Error
	return found(&record, err)
}

func (r *jobRepository) Claim(ctx context.Context, id string) (*imagegen.Job, bool, error) {
	if !validID(id) {
		return nil, false, imagegen.ErrJobNotFound
	}
	var records []schema.ImageGenerationJob
	err := r.db.WithContext(ctx).Raw(`UPDATE image_generation_jobs SET status = ?, attempts = attempts + 1, updated_at = NOW()
		WHERE id = ? AND status = ? RETURNING *`,
		string(imagegen.StatusRunning), id, string(imagegen.StatusQueued)).Scan(&records).Error
	if err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	job, err := toDomain(&records[0])
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (r *jobRepository) MarkDone(ctx context.Context, id string, result imagegen.Result, at time.Time) error {
	return r.finish(r.db.WithContext(ctx).Where("id = ? AND status = ?", id, string(imagegen.StatusRunning)), map[string]any{
		"status":      string(imagegen.StatusDone),
		"media_id":    result.MediaID,
		"media_url":   result.MediaURL,
		"model":       result.Model,
		"finished_at": at,
	})
}

func (r *jobRepository) MarkFailed(ctx context.Context, id string, code imagegen.FailureCode, at time.Time) error {
	if !code.Known() {
		return fmt.Errorf("%w: %q", imagegen.ErrUnknownFailureCode, code)
	}
	return r.finish(r.db.WithContext(ctx).Where("id = ? AND status IN ?", id, unfinished), map[string]any{
		"status":       string(imagegen.StatusFailed),
		"failure_code": string(code),
		"finished_at":  at,
	})
}

func (r *jobRepository) finish(scoped *gorm.DB, fields map[string]any) error {
	result := scoped.Model(&schema.ImageGenerationJob{}).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return imagegen.ErrJobNotActive
	}
	return nil
}

func (r *jobRepository) FailStale(ctx context.Context, createdBefore time.Time, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Raw(`UPDATE image_generation_jobs SET status = ?, failure_code = ?, finished_at = NOW(), updated_at = NOW()
		WHERE id IN (SELECT id FROM image_generation_jobs
			WHERE status IN (?, ?) AND created_at < ?
			ORDER BY created_at LIMIT ? FOR UPDATE SKIP LOCKED)
		AND status IN (?, ?)
		RETURNING id`,
		string(imagegen.StatusFailed), string(imagegen.FailureTimedOut),
		unfinished[0], unfinished[1], createdBefore, limit,
		unfinished[0], unfinished[1]).Scan(&ids).Error
	return ids, err
}

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func found(record *schema.ImageGenerationJob, err error) (*imagegen.Job, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, imagegen.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(record)
}

func toRecord(job *imagegen.Job) *schema.ImageGenerationJob {
	return &schema.ImageGenerationJob{
		ID:                job.ID,
		WorkspaceID:       job.WorkspaceID,
		RequestedBy:       job.RequestedBy,
		Fingerprint:       job.Fingerprint,
		Prompt:            job.Prompt,
		Aspect:            string(job.Aspect),
		ReferenceMediaIDs: storedReferences(job.ReferenceMediaIDs),
		Status:            string(job.Status),
		MediaID:           job.MediaID,
		MediaURL:          job.MediaURL,
		Model:             job.Model,
		FailureCode:       string(job.FailureCode),
		Attempts:          job.Attempts,
		FinishedAt:        job.FinishedAt,
	}
}

func toDomain(r *schema.ImageGenerationJob) (*imagegen.Job, error) {
	status := imagegen.Status(r.Status)
	if !status.Known() {
		return nil, fmt.Errorf("%w: job %s has %q", imagegen.ErrUnknownJobStatus, r.ID, r.Status)
	}
	failure := imagegen.FailureCode(r.FailureCode)
	if failure != "" && !failure.Known() {
		return nil, fmt.Errorf("%w: job %s has %q", imagegen.ErrUnknownFailureCode, r.ID, r.FailureCode)
	}
	return &imagegen.Job{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		RequestedBy:       r.RequestedBy,
		Prompt:            r.Prompt,
		Aspect:            imagegen.Aspect(r.Aspect),
		ReferenceMediaIDs: loadedReferences(r.ReferenceMediaIDs),
		Fingerprint:       r.Fingerprint,
		Status:            status,
		MediaID:           r.MediaID,
		MediaURL:          r.MediaURL,
		Model:             r.Model,
		FailureCode:       failure,
		Attempts:          r.Attempts,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		FinishedAt:        r.FinishedAt,
	}, nil
}

func storedReferences(ids []string) pq.StringArray {
	return append(pq.StringArray{}, ids...)
}

func loadedReferences(ids pq.StringArray) []string {
	if len(ids) == 0 {
		return nil
	}
	return append([]string(nil), ids...)
}
