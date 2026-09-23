package report_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/report"
)

type Service struct {
	repo      report.Repository
	registry  *report.Registry
	publisher messaging.MessageQueuePub
	storage   report.Storage
	retention time.Duration
	now       func() time.Time
}

func NewService(
	repo report.Repository,
	registry *report.Registry,
	publisher messaging.MessageQueuePub,
	storage report.Storage,
) *Service {
	return &Service{
		repo:      repo,
		registry:  registry,
		publisher: publisher,
		storage:   storage,
		retention: report.DefaultRetention,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.now = clock
	}
}

func (s *Service) SetRetention(retention time.Duration) {
	if s != nil && retention > 0 {
		s.retention = retention
	}
}

func (s *Service) Registry() *report.Registry {
	if s == nil {
		return nil
	}
	return s.registry
}

var ErrNotConfigured = errors.New("report: the report service is not configured")

func (s *Service) ready() error {
	if s == nil || s.repo == nil || s.registry == nil || s.publisher == nil || s.storage == nil {
		return ErrNotConfigured
	}
	return nil
}

type CreateInput struct {
	WorkspaceID string
	RequestedBy string
	Kind        report.Kind
	Format      report.Format
	Locale      string
	Params      json.RawMessage
}

func (s *Service) Create(input CreateInput) (*report.Job, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}

	job := &report.Job{
		WorkspaceID: input.WorkspaceID,
		RequestedBy: input.RequestedBy,
		Kind:        input.Kind,
		Format:      input.Format,
		Locale:      input.Locale,
		Params:      input.Params,
		Status:      report.StatusQueued,
	}
	job.Normalize()
	if err := job.Validate(s.registry.Lookup); err != nil {
		return nil, err
	}

	job.Fingerprint = report.Fingerprint(job.WorkspaceID, job.Kind, job.Format, job.Locale, job.Params)

	existing, err := s.repo.FindReusable(job.WorkspaceID, job.Fingerprint, s.now().Add(-report.IdempotencyGrace))
	if err == nil && existing != nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, report.ErrNotFound) {
		return nil, err
	}

	expires := s.now().Add(s.retention)
	job.ExpiresAt = &expires

	if err := s.repo.Create(job); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(report.QueueMessage{
		JobID:       job.ID,
		WorkspaceID: job.WorkspaceID,
		Kind:        job.Kind,
	})
	if err != nil {
		_ = s.repo.MarkFailed(job.ID, report.FailureRenderFailed, s.now())
		return nil, err
	}

	if err := s.publisher.Publish(report.QueueTopic, payload); err != nil {
		_ = s.repo.MarkFailed(job.ID, report.FailureSourceFailed, s.now())
		return nil, err
	}

	return job, nil
}

func (s *Service) Get(workspaceID, id string) (*report.Job, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	job, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	job.Status = job.EffectiveStatus(s.now())
	return job, nil
}

func (s *Service) List(query report.ListQuery) (report.ListPage, error) {
	if err := s.ready(); err != nil {
		return report.ListPage{}, err
	}
	page, err := s.repo.List(query)
	if err != nil {
		return report.ListPage{}, err
	}
	now := s.now()
	for i := range page.Jobs {
		page.Jobs[i].Status = page.Jobs[i].EffectiveStatus(now)
	}
	return page, nil
}

func (s *Service) File(ctx context.Context, workspaceID, id string) (*report.File, error) {
	job, err := s.Get(workspaceID, id)
	if err != nil {
		return nil, err
	}
	switch job.Status {
	case report.StatusDone:
	case report.StatusExpired:
		return nil, report.ErrExpired
	default:
		return nil, report.ErrNotReady
	}
	if job.ObjectKey == "" {
		return nil, report.ErrExpired
	}

	data, contentType, err := s.storage.Download(ctx, job.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("reading the report file: %w", err)
	}
	if contentType == "" {
		contentType = job.Format.ContentType()
	}
	return &report.File{Data: data, ContentType: contentType, Filename: job.Filename}, nil
}

func (s *Service) ExpireOldFiles(limit int) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	return s.repo.ExpireBefore(s.now(), limit)
}

func (s *Service) logf(format string, args ...interface{}) {
	log.Printf("[report] "+format, args...)
}
