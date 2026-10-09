package mediagen_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/mediagen"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

var (
	unfinished = []string{string(mediagen.StatusQueued), string(mediagen.StatusRunning)}
	active     = []string{string(mediagen.StatusQueued), string(mediagen.StatusRunning), string(mediagen.StatusSettling)}
)

type jobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) mediagen.Repository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job *mediagen.Job) error {
	record, err := toRecord(job)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return mediagen.ErrDuplicateActiveJob
		}
		return err
	}
	job.ID, job.CreatedAt, job.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *jobRepository) Get(ctx context.Context, workspaceID, id string) (*mediagen.Job, error) {
	if !validID(id) || !validID(workspaceID) {
		return nil, mediagen.ErrJobNotFound
	}
	var record schema.MediaGenerationJob
	err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&record).Error
	return found(&record, err)
}

func (r *jobRepository) FindActive(ctx context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*mediagen.Job, error) {
	var record schema.MediaGenerationJob
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND requested_by = ? AND fingerprint = ? AND status IN ? AND created_at >= ?",
			workspaceID, requestedBy, fingerprint, active, since).
		Order("created_at DESC").First(&record).Error
	return found(&record, err)
}

func (r *jobRepository) FindDelivered(ctx context.Context, workspaceID string, kind mediagen.Kind, sourceMediaID string) (*mediagen.Job, error) {
	var record schema.MediaGenerationJob
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND kind = ? AND source_media_id = ? AND status = ? AND media_id <> ?",
			workspaceID, string(kind), sourceMediaID, string(mediagen.StatusDone), "").
		Order("updated_at DESC").First(&record).Error
	return found(&record, err)
}

func (r *jobRepository) Claim(ctx context.Context, id string) (*mediagen.Job, bool, error) {
	if !validID(id) {
		return nil, false, mediagen.ErrJobNotFound
	}
	var records []schema.MediaGenerationJob
	err := r.db.WithContext(ctx).Raw(`UPDATE media_generation_jobs SET status = ?, attempts = attempts + 1, updated_at = NOW()
		WHERE id = ? AND status = ? RETURNING *`,
		string(mediagen.StatusRunning), id, string(mediagen.StatusQueued)).Scan(&records).Error
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

func (r *jobRepository) MarkDone(ctx context.Context, id string, result mediagen.Result, at time.Time) error {
	return r.finish(r.db.WithContext(ctx).Where("id = ? AND status = ?", id, string(mediagen.StatusRunning)), map[string]any{
		"status":      string(mediagen.StatusDone),
		"media_id":    result.MediaID,
		"media_url":   result.MediaURL,
		"model":       result.Model,
		"finished_at": at,
	})
}

func (r *jobRepository) MarkFailed(ctx context.Context, id string, code mediagen.FailureCode, at time.Time) error {
	if !code.Known() {
		return fmt.Errorf("%w: %q", mediagen.ErrUnknownFailureCode, code)
	}
	return r.finish(r.db.WithContext(ctx).Where("id = ? AND status IN ?", id, unfinished), map[string]any{
		"status":       string(mediagen.StatusFailed),
		"failure_code": string(code),
		"finished_at":  at,
	})
}

func (r *jobRepository) finish(scoped *gorm.DB, fields map[string]any) error {
	result := scoped.Model(&schema.MediaGenerationJob{}).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return mediagen.ErrJobNotActive
	}
	return nil
}

func (r *jobRepository) FailStale(ctx context.Context, createdBefore time.Time, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Raw(`UPDATE media_generation_jobs SET status = ?, failure_code = ?, finished_at = NOW(), updated_at = NOW()
		WHERE id IN (SELECT id FROM media_generation_jobs
			WHERE status IN (?, ?) AND created_at < ?
			ORDER BY created_at LIMIT ? FOR UPDATE SKIP LOCKED)
		AND status IN (?, ?)
		RETURNING id`,
		string(mediagen.StatusFailed), string(mediagen.FailureTimedOut),
		unfinished[0], unfinished[1], createdBefore, limit,
		unfinished[0], unfinished[1]).Scan(&ids).Error
	return ids, err
}

func (r *jobRepository) MarkSettling(ctx context.Context, id string, settlement mediagen.Settlement, at time.Time) error {
	if err := settlement.Validate(); err != nil {
		return err
	}
	fields := map[string]any{"status": string(mediagen.StatusSettling), "generation_id": settlement.GenerationID, "updated_at": at}
	if settlement.Result != nil {
		fields["media_id"], fields["media_url"], fields["model"] = settlement.Result.MediaID, settlement.Result.MediaURL, settlement.Result.Model
	} else {
		fields["failure_code"] = string(settlement.Failure)
	}
	return r.finish(r.db.WithContext(ctx).Where("id = ? AND status = ?", id, string(mediagen.StatusRunning)), fields)
}

func (r *jobRepository) ListSettling(ctx context.Context, createdAfter time.Time, limit int) ([]*mediagen.Job, error) {
	var records []schema.MediaGenerationJob
	err := r.db.WithContext(ctx).Where("status = ? AND created_at >= ?", string(mediagen.StatusSettling), createdAfter).
		Order("created_at").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}
	jobs := make([]*mediagen.Job, 0, len(records))
	for i := range records {
		job, err := toDomain(&records[i])
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *jobRepository) Settle(ctx context.Context, id string, at time.Time) (*mediagen.Job, bool, error) {
	if !validID(id) {
		return nil, false, mediagen.ErrJobNotFound
	}
	var records []schema.MediaGenerationJob
	err := r.db.WithContext(ctx).Raw(`UPDATE media_generation_jobs
		SET status = CASE WHEN media_id <> '' THEN ? ELSE ? END, finished_at = ?, updated_at = NOW()
		WHERE id = ? AND status = ? RETURNING *`,
		string(mediagen.StatusDone), string(mediagen.StatusFailed), at, id, string(mediagen.StatusSettling)).Scan(&records).Error
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

func (r *jobRepository) ExpireSettling(ctx context.Context, createdBefore time.Time, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Raw(`UPDATE media_generation_jobs SET status = ?, failure_code = ?, finished_at = NOW(), updated_at = NOW()
		WHERE id IN (SELECT id FROM media_generation_jobs
			WHERE status = ? AND created_at < ?
			ORDER BY created_at LIMIT ? FOR UPDATE SKIP LOCKED)
		AND status = ?
		RETURNING id`,
		string(mediagen.StatusFailed), string(mediagen.FailureCostUnreported),
		string(mediagen.StatusSettling), createdBefore, limit, string(mediagen.StatusSettling)).Scan(&ids).Error
	return ids, err
}

func (r *jobRepository) CountActive(ctx context.Context, workspaceID string, kinds []mediagen.Kind, since time.Time) (int, error) {
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&schema.MediaGenerationJob{}).
		Where("workspace_id = ? AND kind IN ? AND status IN ? AND created_at >= ?", workspaceID, names, unfinished, since).
		Count(&count).Error
	return int(count), err
}

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func found(record *schema.MediaGenerationJob, err error) (*mediagen.Job, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mediagen.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(record)
}

func toRecord(job *mediagen.Job) (*schema.MediaGenerationJob, error) {
	video, err := storedVideo(job)
	if err != nil {
		return nil, err
	}
	return &schema.MediaGenerationJob{
		ID:                job.ID,
		WorkspaceID:       job.WorkspaceID,
		RequestedBy:       job.RequestedBy,
		Fingerprint:       job.Fingerprint,
		Kind:              string(job.Kind),
		Prompt:            job.Prompt,
		Aspect:            string(job.Aspect),
		ReferenceMediaIDs: storedReferences(job.ReferenceMediaIDs),
		Voice:             job.Voice,
		Video:             video,
		SourceMediaID:     job.SourceMediaID,
		BillingReference:  job.BillingReference,
		Status:            string(job.Status),
		MediaID:           job.MediaID,
		MediaURL:          job.MediaURL,
		Model:             job.Model,
		GenerationID:      job.GenerationID,
		FailureCode:       string(job.FailureCode),
		Attempts:          job.Attempts,
		FinishedAt:        job.FinishedAt,
	}, nil
}

func storedVideo(job *mediagen.Job) (datatypes.JSON, error) {
	if job.Kind != mediagen.KindVideo {
		return nil, nil
	}
	return json.Marshal(job.Video)
}

func loadedVideo(r *schema.MediaGenerationJob, kind mediagen.Kind) (mediagen.Timeline, error) {
	var video mediagen.Timeline
	if kind != mediagen.KindVideo {
		return video, nil
	}
	if err := json.Unmarshal(r.Video, &video); err != nil {
		return video, fmt.Errorf("mediagen repository: job %s has an unreadable video spec: %w", r.ID, err)
	}
	return video, nil
}

func toDomain(r *schema.MediaGenerationJob) (*mediagen.Job, error) {
	status := mediagen.Status(r.Status)
	if !status.Known() {
		return nil, fmt.Errorf("%w: job %s has %q", mediagen.ErrUnknownJobStatus, r.ID, r.Status)
	}
	kind := mediagen.Kind(r.Kind)
	if !kind.Known() {
		return nil, fmt.Errorf("%w: job %s has %q", mediagen.ErrUnknownKind, r.ID, r.Kind)
	}
	video, err := loadedVideo(r, kind)
	if err != nil {
		return nil, err
	}
	failure := mediagen.FailureCode(r.FailureCode)
	if failure != "" && !failure.Known() {
		return nil, fmt.Errorf("%w: job %s has %q", mediagen.ErrUnknownFailureCode, r.ID, r.FailureCode)
	}
	return &mediagen.Job{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		RequestedBy:       r.RequestedBy,
		Kind:              kind,
		Prompt:            r.Prompt,
		Aspect:            mediagen.Aspect(r.Aspect),
		ReferenceMediaIDs: loadedReferences(r.ReferenceMediaIDs),
		Voice:             r.Voice,
		Video:             video,
		SourceMediaID:     r.SourceMediaID,
		BillingReference:  r.BillingReference,
		Fingerprint:       r.Fingerprint,
		Status:            status,
		MediaID:           r.MediaID,
		MediaURL:          r.MediaURL,
		Model:             r.Model,
		GenerationID:      r.GenerationID,
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
