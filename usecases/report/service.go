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
	"vozko/domain/workspace"
)

type Access = workspace.PermissionChecker

type Viewer struct {
	UserID  string
	IsAdmin bool
}

type Service struct {
	repo      report.Repository
	registry  *report.Registry
	publisher messaging.MessageQueuePub
	storage   report.Storage
	retention time.Duration
	now       func() time.Time
	access    Access

	printSecret string
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

func (s *Service) SetAccess(access Access) {
	if s != nil {
		s.access = access
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
	if s == nil || s.repo == nil || s.registry == nil || s.publisher == nil || s.storage == nil || s.access == nil {
		return ErrNotConfigured
	}
	return nil
}

type CreateInput struct {
	WorkspaceID      string
	RequestedBy      string
	RequestedByAdmin bool
	Kind             report.Kind
	Format           report.Format
	Locale           string
	Params           json.RawMessage
	FromClient       bool
}

func (s *Service) Create(input CreateInput) (*report.Job, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}

	job := &report.Job{
		WorkspaceID:      input.WorkspaceID,
		RequestedBy:      input.RequestedBy,
		RequestedByAdmin: input.RequestedByAdmin,
		Kind:             input.Kind,
		Format:           input.Format,
		Locale:           input.Locale,
		Params:           input.Params,
		Status:           report.StatusQueued,
	}
	job.Normalize()
	if err := job.Validate(s.registry.Lookup); err != nil {
		return nil, err
	}

	renderer, _ := s.registry.Lookup(job.Kind)
	if input.FromClient && report.IsInternal(renderer) {
		return nil, report.ErrKindNotOffered
	}
	policy, err := report.PolicyOf(renderer, *job)
	if err != nil {
		return nil, err
	}
	if !policy.Allows(s.holdsFor(job.WorkspaceID, job.RequestedBy, job.RequestedByAdmin)) {
		return nil, report.ErrNotAllowed
	}

	job.Readers = policy.Readers()
	job.Fingerprint = report.Fingerprint(job.WorkspaceID, job.RequestedBy, policy.Tier, job.Kind, job.Format, job.Locale, job.Params)

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

	if err := s.publisher.Publish(report.TopicFor(job.Format), payload); err != nil {
		_ = s.repo.MarkFailed(job.ID, report.FailureSourceFailed, s.now())
		return nil, err
	}

	return job, nil
}

func (s *Service) holdsFor(workspaceID, userID string, isAdmin bool) func(workspace.PermissionEntry) bool {
	return func(p workspace.PermissionEntry) bool {
		return workspace.HoldsAll(s.access, workspaceID, userID, isAdmin, []workspace.PermissionEntry{p})
	}
}

func (s *Service) readable(v Viewer, job report.Job) bool {
	renderer, found := s.registry.Lookup(job.Kind)
	if !found {
		return job.ReadableBy(v.UserID, report.Policy{}, nil)
	}
	policy, err := report.PolicyOf(renderer, job)
	if err != nil {
		return job.ReadableBy(v.UserID, report.Policy{}, nil)
	}
	return job.ReadableBy(v.UserID, policy, s.holdsFor(job.WorkspaceID, v.UserID, v.IsAdmin))
}

func (s *Service) Get(v Viewer, workspaceID, id string) (*report.Job, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	job, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !s.readable(v, *job) {
		return nil, report.ErrNotFound
	}
	job.Status = job.EffectiveStatus(s.now())
	return job, nil
}

func (s *Service) List(v Viewer, query report.ListQuery) (report.ListPage, error) {
	if err := s.ready(); err != nil {
		return report.ListPage{}, err
	}
	held, err := s.heldReaderKeys(v, query.WorkspaceID)
	if err != nil {
		return report.ListPage{}, err
	}
	query.Viewer, query.ViewerHolds = v.UserID, held
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

func (s *Service) heldReaderKeys(v Viewer, workspaceID string) ([]string, error) {
	keys, err := s.repo.ReaderKeys(workspaceID)
	if err != nil {
		return nil, err
	}
	holds := s.holdsFor(workspaceID, v.UserID, v.IsAdmin)
	held := make([]string, 0, len(keys))
	for _, key := range keys {
		entries, err := workspace.ParsePermissionEntries([]string{key})
		if err != nil {
			continue
		}
		if holds(entries[0]) {
			held = append(held, key)
		}
	}
	return held, nil
}

func (s *Service) File(ctx context.Context, v Viewer, workspaceID, id string) (*report.File, error) {
	job, err := s.Get(v, workspaceID, id)
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

func (s *Service) SetPrintSecret(secret string) {
	if s != nil {
		s.printSecret = secret
	}
}

type PrintPayload struct {
	Job  report.Job  `json:"job"`
	Data interface{} `json:"data"`
}

func (s *Service) PrintPayload(ctx context.Context, token string) (*PrintPayload, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}

	grant, err := report.VerifyPrintToken(s.printSecret, token, s.now())
	if err != nil {
		return nil, err
	}

	job, err := s.repo.GetByID(grant.WorkspaceID, grant.JobID)
	if err != nil {
		return nil, err
	}

	renderer, found := s.registry.Lookup(job.Kind)
	if !found {
		return nil, report.ErrNoRenderer
	}
	provider, ok := renderer.(report.PrintDataProvider)
	if !ok {
		return nil, report.ErrNoRenderer
	}

	data, err := provider.PrintData(ctx, *job)
	if err != nil {
		return nil, err
	}
	return &PrintPayload{Job: *job, Data: data}, nil
}
