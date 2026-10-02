package imagegen_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/imagegen"
	"vozko/domain/media"
)

var (
	ErrInvalidCostCeiling = errors.New("imagegen: the unknown cost ceiling must be positive")
	ErrMissingDependency  = errors.New("imagegen: the service is missing a dependency")
)

const (
	reapBatchSize    = 100
	storedImageLabel = "Imagem gerada com IA"
	settleTimeout    = 10 * time.Second
)

type FundsGate interface {
	Check(workspaceID string) error
}

type AIBilling interface {
	Publish(workspaceID, model string, promptTokens, completionTokens int, providerCostMicros int64)
}

type MediaUploader interface {
	UploadMedia(workspaceID string, data []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error)
}

type ReferenceLibrary interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type Deps struct {
	Generator         imagegen.Generator
	Jobs              imagegen.Repository
	Queue             imagegen.Queue
	Funds             FundsGate
	Billing           AIBilling
	Uploader          MediaUploader
	References        ReferenceLibrary
	CostCeilingMicros int64
}

type Service struct {
	d    Deps
	now  func() time.Time
	poll pollPolicy
}

func NewService(d Deps) (*Service, error) {
	if d.Generator == nil || d.Jobs == nil || d.Queue == nil || d.Funds == nil || d.Billing == nil || d.Uploader == nil || d.References == nil {
		return nil, ErrMissingDependency
	}
	if d.CostCeilingMicros <= 0 {
		return nil, ErrInvalidCostCeiling
	}
	return &Service{d: d, now: func() time.Time { return time.Now().UTC() }, poll: defaultPoll}, nil
}

func (s *Service) Check(req imagegen.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if _, err := s.References(req.WorkspaceID, req.ReferenceMediaIDs); err != nil {
		return err
	}
	return s.d.Funds.Check(req.WorkspaceID)
}

func (s *Service) References(workspaceID string, ids []string) ([]imagegen.ReferenceImage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	refs := make([]imagegen.ReferenceImage, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		found, err := s.d.References.GetMedia(workspaceID, id)
		if err != nil && !errors.Is(err, media.ErrMediaNotFound) {
			return nil, fmt.Errorf("imagegen: load reference %s: %w", id, err)
		}
		if err != nil || found == nil {
			return nil, referenceIssue(imagegen.CodeNotFound)
		}
		if found.Type != media.MediaTypeProductImage || strings.TrimSpace(found.URL) == "" {
			return nil, referenceIssue(imagegen.CodeNotImage)
		}
		refs = append(refs, imagegen.ReferenceImage{MediaID: found.ID, URL: found.URL})
	}
	return refs, nil
}

func referenceIssue(code string) error {
	return &imagegen.ValidationError{Issues: []imagegen.FieldIssue{{Field: imagegen.FieldReferences, Code: code}}}
}

func (s *Service) Request(ctx context.Context, req imagegen.Request, requestedBy string) (*imagegen.Job, error) {
	job, err := imagegen.NewJob(req, requestedBy)
	if err != nil {
		return nil, err
	}
	if _, err := s.References(job.WorkspaceID, job.ReferenceMediaIDs); err != nil {
		return nil, err
	}
	if err := s.d.Funds.Check(job.WorkspaceID); err != nil {
		return nil, err
	}
	active, err := s.d.Jobs.FindActive(ctx, job.WorkspaceID, job.RequestedBy, job.Fingerprint, imagegen.ActiveSince(s.now()))
	if err == nil {
		return active, nil
	}
	if !errors.Is(err, imagegen.ErrJobNotFound) {
		return nil, err
	}
	if err := s.d.Jobs.Create(ctx, job); err != nil {
		if errors.Is(err, imagegen.ErrDuplicateActiveJob) {
			return s.winnerOf(ctx, job)
		}
		return nil, err
	}
	if err := s.d.Queue.Enqueue(job.ID); err != nil {
		s.fail(ctx, job, imagegen.FailureEnqueue)
		return nil, fmt.Errorf("imagegen: queue job %s: %w", job.ID, err)
	}
	return job, nil
}

func (s *Service) winnerOf(ctx context.Context, job *imagegen.Job) (*imagegen.Job, error) {
	winner, err := s.d.Jobs.FindActive(ctx, job.WorkspaceID, job.RequestedBy, job.Fingerprint, time.Time{})
	if errors.Is(err, imagegen.ErrJobNotFound) {
		return nil, imagegen.ErrDuplicateActiveJob
	}
	return winner, err
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*imagegen.Job, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, imagegen.ErrWorkspaceRequired
	}
	if strings.TrimSpace(id) == "" {
		return nil, imagegen.ErrJobNotFound
	}
	return s.d.Jobs.Get(ctx, workspaceID, id)
}

func (s *Service) Process(ctx context.Context, msg *imagegen.QueueMessage) error {
	if msg == nil || strings.TrimSpace(msg.JobID) == "" {
		return imagegen.ErrJobNotFound
	}
	job, claimed, err := s.d.Jobs.Claim(ctx, msg.JobID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	s.run(ctx, job)
	return nil
}

func (s *Service) run(ctx context.Context, job *imagegen.Job) {
	if err := s.d.Funds.Check(job.WorkspaceID); err != nil {
		log.Printf("[image-generation] job %s stopped by the funds check: %v", job.ID, err)
		s.fail(ctx, job, imagegen.FailureInsufficientFunds)
		return
	}
	references, err := s.References(job.WorkspaceID, job.ReferenceMediaIDs)
	if err != nil {
		log.Printf("[image-generation] job %s could not load its references: %v", job.ID, err)
		s.fail(ctx, job, referenceFailure(err))
		return
	}
	image, err := s.d.Generator.Generate(ctx, job.Request(), references)
	if err != nil {
		log.Printf("[image-generation] job %s could not generate: %v", job.ID, err)
		s.fail(ctx, job, imagegen.FailureGeneration)
		return
	}
	s.bill(job, image)
	name := "images/" + job.WorkspaceID + "/" + uuid.NewString() + ".jpg"
	stored, err := s.d.Uploader.UploadMedia(job.WorkspaceID, image.Bytes, name, media.MediaTypeProductImage, storedImageLabel)
	if err != nil {
		log.Printf("[image-generation] job %s for workspace %s was billed but the image could not be stored: %v", job.ID, job.WorkspaceID, err)
		s.fail(ctx, job, imagegen.FailureStorage)
		return
	}
	result := imagegen.Result{MediaID: stored.ID, MediaURL: stored.URL, Model: image.Model}
	settleCtx, cancel := settling(ctx)
	defer cancel()
	if err := s.d.Jobs.MarkDone(settleCtx, job.ID, result, s.now()); err != nil {
		log.Printf("[image-generation] job %s stored media %s but could not be marked done: %v", job.ID, stored.ID, err)
	}
}

func referenceFailure(err error) imagegen.FailureCode {
	var invalid *imagegen.ValidationError
	if errors.As(err, &invalid) {
		return imagegen.FailureReferenceUnavailable
	}
	return imagegen.FailureGeneration
}

func (s *Service) bill(job *imagegen.Job, image *imagegen.GeneratedImage) {
	cost := image.ProviderCostMicros
	if cost <= 0 {
		log.Printf("[image-generation] job %s for workspace %s reported no cost; billing the ceiling of %d micros", job.ID, job.WorkspaceID, s.d.CostCeilingMicros)
		cost = s.d.CostCeilingMicros
	}
	s.d.Billing.Publish(job.WorkspaceID, image.Model, 0, 0, cost)
}

func (s *Service) fail(ctx context.Context, job *imagegen.Job, code imagegen.FailureCode) {
	at := s.now()
	settleCtx, cancel := settling(ctx)
	defer cancel()
	if err := s.d.Jobs.MarkFailed(settleCtx, job.ID, code, at); err != nil {
		log.Printf("[image-generation] job %s could not be marked %s: %v", job.ID, code, err)
		return
	}
	job.Status, job.FailureCode, job.FinishedAt = imagegen.StatusFailed, code, &at
}

func (s *Service) Reap(ctx context.Context) error {
	ids, err := s.d.Jobs.FailStale(ctx, imagegen.ActiveSince(s.now()), reapBatchSize)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		log.Printf("[image-generation] timed out %d job(s): %v", len(ids), ids)
	}
	return nil
}

type pollPolicy struct {
	first  time.Duration
	factor float64
	max    time.Duration
}

var defaultPoll = pollPolicy{first: time.Second, factor: 1.5, max: 5 * time.Second}

func (p pollPolicy) next(delay time.Duration) time.Duration {
	next := time.Duration(float64(delay) * p.factor)
	if next > p.max {
		return p.max
	}
	return next
}

func (s *Service) Wait(ctx context.Context, workspaceID, id string) (*imagegen.Job, error) {
	delay := s.poll.first
	for {
		job, err := s.Get(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if !job.Status.Known() {
			return nil, fmt.Errorf("%w: %q", imagegen.ErrUnknownJobStatus, job.Status)
		}
		if job.Status.Terminal() {
			return job, nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return job, ctx.Err()
		case <-timer.C:
		}
		delay = s.poll.next(delay)
	}
}

func settling(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}
