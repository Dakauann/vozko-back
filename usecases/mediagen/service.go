package mediagen_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/mediagen"
)

var ErrMissingDependency = errors.New("mediagen: the service is missing a dependency")

const (
	reapBatchSize = 100
	settleTimeout = 10 * time.Second
)

type FundsGate interface {
	Check(workspaceID string) error
}

type AIBilling interface {
	PublishFor(reference, workspaceID, model string, promptTokens, completionTokens int, providerCostMicros int64)
}

type MediaUploader interface {
	UploadMedia(workspaceID string, data []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error)
}

type MediaLibrary interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type Deps struct {
	Generators map[mediagen.Kind]mediagen.Generator
	Models     mediagen.ModelCatalog
	Jobs       mediagen.Repository
	Queue      mediagen.Queue
	Funds      FundsGate
	Billing    AIBilling
	Uploader   MediaUploader
	Library    MediaLibrary
	Costs      mediagen.CostLookup
	LateCosts  mediagen.CostLookup
	Charges    mediagen.ProcessingCharges
}

type Service struct {
	d    Deps
	now  func() time.Time
	poll pollPolicy
}

func NewService(d Deps) (*Service, error) {
	if d.Models == nil || d.Jobs == nil || d.Queue == nil || d.Funds == nil || d.Billing == nil || d.Uploader == nil || d.Library == nil || d.Costs == nil || d.LateCosts == nil || d.Charges == nil {
		return nil, ErrMissingDependency
	}
	for _, kind := range mediagen.Kinds() {
		if d.Generators[kind] == nil {
			return nil, fmt.Errorf("%w: no generator for %s", ErrMissingDependency, kind)
		}
	}
	return &Service{d: d, now: func() time.Time { return time.Now().UTC() }, poll: defaultPoll}, nil
}

func (s *Service) Models(ctx context.Context, kind mediagen.Kind) ([]mediagen.Model, error) {
	if !kind.UsesModel() {
		return nil, mediagen.ErrNoModels
	}
	models, err := s.d.Models.Models(ctx, kind)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", mediagen.ErrModelsUnavailable, err)
	}
	if len(models) == 0 {
		return nil, mediagen.ErrNoModels
	}
	return models, nil
}

func (s *Service) DefaultModel(ctx context.Context, kind mediagen.Kind) (mediagen.Model, error) {
	models, err := s.Models(ctx, kind)
	if err != nil {
		return mediagen.Model{}, err
	}
	model, _ := mediagen.DefaultModel(models)
	return model, nil
}

func (s *Service) Generates(ctx context.Context, kind mediagen.Kind, model string) (bool, error) {
	if strings.TrimSpace(model) == "" {
		return false, nil
	}
	models, err := s.Models(ctx, kind)
	if err != nil {
		return false, err
	}
	return mediagen.Supported(models, model) == nil, nil
}

func (s *Service) supports(ctx context.Context, req mediagen.Request) error {
	if !req.Kind.UsesModel() {
		return nil
	}
	models, err := s.Models(ctx, req.Kind)
	if err != nil {
		return err
	}
	return mediagen.Supported(models, req.Model)
}

func (s *Service) Check(ctx context.Context, req mediagen.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := s.supports(ctx, req); err != nil {
		return err
	}
	return s.checkSources(req)
}

func (s *Service) CheckContent(req mediagen.Request) error {
	if err := req.ValidateContent(); err != nil {
		return err
	}
	return s.checkSources(req)
}

func (s *Service) checkSources(req mediagen.Request) error {
	if _, err := s.Sources(req); err != nil {
		return err
	}
	return s.d.Funds.Check(req.WorkspaceID)
}

func (s *Service) Sources(req mediagen.Request) ([]mediagen.Source, error) {
	roles := req.SourceRoles()
	if len(roles) == 0 {
		return nil, nil
	}
	sources := make([]mediagen.Source, 0, len(roles))
	for _, role := range roles {
		found, err := s.d.Library.GetMedia(req.WorkspaceID, role.MediaID)
		if err != nil && !errors.Is(err, media.ErrMediaNotFound) {
			return nil, fmt.Errorf("mediagen: load source %s: %w", role.MediaID, err)
		}
		if err != nil || found == nil {
			return nil, sourceIssue(role.Field, mediagen.CodeNotFound)
		}
		if !role.Accepted(found.Type) || strings.TrimSpace(found.URL) == "" {
			return nil, sourceIssue(role.Field, role.Mismatch)
		}
		sources = append(sources, mediagen.Source{MediaID: found.ID, URL: found.URL, Type: found.Type})
	}
	return sources, nil
}

func sourceIssue(field, code string) error {
	return &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: field, Code: code}}}
}

func (s *Service) Request(ctx context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error) {
	job, err := mediagen.NewJob(req, requestedBy)
	if err != nil {
		return nil, err
	}
	if err := s.supports(ctx, job.Request()); err != nil {
		return nil, err
	}
	if err := s.checkSources(job.Request()); err != nil {
		return nil, err
	}
	if delivered, err := s.reused(ctx, job); delivered != nil || err != nil {
		return delivered, err
	}
	active, err := s.d.Jobs.FindActive(ctx, job.WorkspaceID, job.RequestedBy, job.Fingerprint, mediagen.ActiveSince(s.now()))
	if err == nil && !active.Outcome().Terminal() {
		return active, nil
	}
	if err != nil && !errors.Is(err, mediagen.ErrJobNotFound) {
		return nil, err
	}
	if err := s.withinActiveCap(ctx, job); err != nil {
		return nil, err
	}
	if err := s.d.Jobs.Create(ctx, job); err != nil {
		if errors.Is(err, mediagen.ErrDuplicateActiveJob) {
			return s.winnerOf(ctx, job)
		}
		return nil, err
	}
	if err := s.d.Queue.Enqueue(job); err != nil {
		s.fail(ctx, job, mediagen.FailureEnqueue, mediagen.FailureDetail(err))
		return nil, fmt.Errorf("mediagen: queue job %s: %w", job.ID, err)
	}
	return job, nil
}

func (s *Service) reused(ctx context.Context, job *mediagen.Job) (*mediagen.Job, error) {
	if !job.Kind.Reusable() {
		return nil, nil
	}
	delivered, err := s.d.Jobs.FindDelivered(ctx, job.WorkspaceID, job.Kind, job.SourceMediaID)
	if errors.Is(err, mediagen.ErrJobNotFound) {
		return nil, nil
	}
	return delivered, err
}

func (s *Service) withinActiveCap(ctx context.Context, job *mediagen.Job) error {
	kinds, ceiling := mediagen.ProcessingKinds(), mediagen.MaxActiveProcessing
	if job.Kind.UsesModel() {
		kinds, ceiling = mediagen.GenerationKinds(), mediagen.MaxActiveGenerations
	}
	active, err := s.d.Jobs.CountActive(ctx, job.WorkspaceID, kinds, mediagen.ActiveSince(s.now()))
	if err != nil {
		return err
	}
	if active >= ceiling {
		return mediagen.ErrTooManyActive
	}
	return nil
}

func (s *Service) winnerOf(ctx context.Context, job *mediagen.Job) (*mediagen.Job, error) {
	winner, err := s.d.Jobs.FindActive(ctx, job.WorkspaceID, job.RequestedBy, job.Fingerprint, time.Time{})
	if errors.Is(err, mediagen.ErrJobNotFound) {
		return nil, mediagen.ErrDuplicateActiveJob
	}
	return winner, err
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*mediagen.Job, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, mediagen.ErrWorkspaceRequired
	}
	if strings.TrimSpace(id) == "" {
		return nil, mediagen.ErrJobNotFound
	}
	return s.d.Jobs.Get(ctx, workspaceID, id)
}

func (s *Service) Process(ctx context.Context, msg *mediagen.QueueMessage) error {
	if msg == nil || strings.TrimSpace(msg.JobID) == "" {
		return mediagen.ErrJobNotFound
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

func (s *Service) run(ctx context.Context, job *mediagen.Job) {
	storage, known := job.Kind.Storage()
	generator := s.d.Generators[job.Kind]
	if !known || generator == nil {
		missing := fmt.Errorf("kind %q has no generator", job.Kind)
		log.Printf("[media-generation] job %s: %v", job.ID, missing)
		s.fail(ctx, job, mediagen.FailureGeneration, mediagen.FailureDetail(missing))
		return
	}
	if err := s.d.Funds.Check(job.WorkspaceID); err != nil {
		log.Printf("[media-generation] job %s stopped by the funds check: %v", job.ID, err)
		s.fail(ctx, job, mediagen.FailureInsufficientFunds, mediagen.FailureDetail(err))
		return
	}
	sources, err := s.Sources(job.Request())
	if err != nil {
		log.Printf("[media-generation] job %s could not load its sources: %v", job.ID, err)
		s.fail(ctx, job, sourceFailure(err), mediagen.FailureDetail(err))
		return
	}
	output, err := generator.Generate(ctx, job.Request(), sources)
	if err != nil {
		s.generationFailed(ctx, job, err)
		return
	}
	priced := s.price(ctx, job, output)
	if !priced && output.GenerationID == "" {
		log.Printf("CRITICAL: [media-generation] job %s for workspace %s: the provider reported neither a cost nor a generation id; nothing stored or billed", job.ID, job.WorkspaceID)
		s.fail(ctx, job, mediagen.FailureCostUnreported, mediagen.FailureDetail(mediagen.ErrCostUnreported))
		return
	}
	if job.Kind.Processing() {
		if err := s.d.Charges.Charge(ctx, job); err != nil {
			log.Printf("[media-generation] job %s for workspace %s could not be charged: %v; nothing delivered", job.ID, job.WorkspaceID, err)
			s.fail(ctx, job, mediagen.FailureInsufficientFunds, mediagen.FailureDetail(err))
			return
		}
	}
	if priced {
		s.bill(job, output)
	}
	name := storage.Folder + "/" + job.WorkspaceID + "/" + uuid.NewString() + storage.Extension
	stored, err := s.d.Uploader.UploadMedia(job.WorkspaceID, output.Bytes, name, storage.Type, storage.Label)
	if err != nil {
		log.Printf("[media-generation] job %s for workspace %s produced %s but it could not be stored: %v", job.ID, job.WorkspaceID, job.Kind, err)
		s.closeUnpriced(ctx, job, priced, mediagen.Settlement{GenerationID: output.GenerationID, Failure: mediagen.FailureStorage, Detail: mediagen.FailureDetail(err)})
		return
	}
	result := mediagen.Result{MediaID: stored.ID, MediaURL: stored.URL, Model: output.Model}
	if !priced {
		s.settleLater(ctx, job, mediagen.Settlement{GenerationID: output.GenerationID, Result: &result})
		return
	}
	settleCtx, cancel := settling(ctx)
	defer cancel()
	if err := s.d.Jobs.MarkDone(settleCtx, job.ID, result, s.now()); err != nil {
		log.Printf("[media-generation] job %s stored media %s but could not be marked done: %v", job.ID, stored.ID, err)
	}
}

func (s *Service) price(ctx context.Context, job *mediagen.Job, output *mediagen.Output) bool {
	if output.Billable(job.Kind) == nil {
		return true
	}
	if output.GenerationID == "" {
		return false
	}
	cost, ok := s.d.Costs.CostMicros(ctx, output.GenerationID)
	if !ok {
		return false
	}
	output.ProviderCostMicros, output.CostReported = cost, true
	return true
}

func (s *Service) generationFailed(ctx context.Context, job *mediagen.Job, err error) {
	log.Printf("[media-generation] job %s could not generate: %v", job.ID, err)
	var charged *mediagen.ChargedFailure
	detail := mediagen.FailureDetail(err)
	if !job.Kind.UsesModel() || !errors.As(err, &charged) {
		s.fail(ctx, job, mediagen.FailureGeneration, detail)
		return
	}
	cost, ok := s.d.Costs.CostMicros(ctx, charged.GenerationID)
	if ok {
		s.d.Billing.PublishFor(job.BillingReference, job.WorkspaceID, job.Model, 0, 0, cost)
		s.fail(ctx, job, mediagen.FailureGeneration, detail)
		return
	}
	s.settleLater(ctx, job, mediagen.Settlement{GenerationID: charged.GenerationID, Failure: mediagen.FailureGeneration, Detail: detail})
}

func (s *Service) closeUnpriced(ctx context.Context, job *mediagen.Job, priced bool, settlement mediagen.Settlement) {
	if priced {
		s.fail(ctx, job, settlement.Failure, settlement.Detail)
		return
	}
	s.settleLater(ctx, job, settlement)
}

func (s *Service) settleLater(ctx context.Context, job *mediagen.Job, settlement mediagen.Settlement) {
	settleCtx, cancel := settling(ctx)
	defer cancel()
	if err := s.d.Jobs.MarkSettling(settleCtx, job.ID, settlement, s.now()); err != nil {
		log.Printf("CRITICAL: [media-generation] job %s for workspace %s could not wait for its cost (generation %s): %v", job.ID, job.WorkspaceID, settlement.GenerationID, err)
		return
	}
	log.Printf("[media-generation] job %s is waiting for the provider to report the cost of generation %s", job.ID, settlement.GenerationID)
}

func (s *Service) Settle(ctx context.Context) error {
	jobs, err := s.d.Jobs.ListSettling(ctx, mediagen.SettleSince(s.now()), reapBatchSize)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		cost, ok := s.d.LateCosts.CostMicros(ctx, job.GenerationID)
		if !ok {
			continue
		}
		settled, won, err := s.d.Jobs.Settle(ctx, job.ID, s.now())
		if err != nil {
			log.Printf("[media-generation] job %s has its cost but could not be settled: %v", job.ID, err)
			continue
		}
		if won {
			s.d.Billing.PublishFor(settled.BillingReference, settled.WorkspaceID, settled.Model, 0, 0, cost)
		}
	}
	expired, err := s.d.Jobs.ExpireSettling(ctx, mediagen.SettleSince(s.now()), reapBatchSize)
	if err != nil {
		return err
	}
	if len(expired) > 0 {
		log.Printf("CRITICAL: [media-generation] %d job(s) never got a provider cost within %s and were closed unbilled: %v", len(expired), mediagen.SettleWindow, expired)
	}
	return nil
}

func sourceFailure(err error) mediagen.FailureCode {
	var invalid *mediagen.ValidationError
	if errors.As(err, &invalid) {
		return mediagen.FailureReferenceUnavailable
	}
	return mediagen.FailureGeneration
}

func (s *Service) bill(job *mediagen.Job, output *mediagen.Output) {
	if !job.Kind.UsesModel() {
		return
	}
	s.d.Billing.PublishFor(job.BillingReference, job.WorkspaceID, output.Model, 0, 0, output.ProviderCostMicros)
}

func (s *Service) fail(ctx context.Context, job *mediagen.Job, code mediagen.FailureCode, detail string) {
	at := s.now()
	settleCtx, cancel := settling(ctx)
	defer cancel()
	if err := s.d.Jobs.MarkFailed(settleCtx, job.ID, code, detail, at); err != nil {
		log.Printf("[media-generation] job %s could not be marked %s: %v", job.ID, code, err)
		return
	}
	job.Status, job.FailureCode, job.FailureDetail, job.FinishedAt = mediagen.StatusFailed, code, detail, &at
}

func (s *Service) Reap(ctx context.Context) error {
	ids, err := s.d.Jobs.FailStale(ctx, mediagen.ActiveSince(s.now()), reapBatchSize)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		log.Printf("[media-generation] timed out %d job(s): %v", len(ids), ids)
	}
	return s.Settle(ctx)
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

func (s *Service) Wait(ctx context.Context, workspaceID, id string) (*mediagen.Job, error) {
	delay := s.poll.first
	for {
		job, err := s.Get(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if !job.Status.Known() {
			return nil, fmt.Errorf("%w: %q", mediagen.ErrUnknownJobStatus, job.Status)
		}
		if job.Outcome().Terminal() {
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

func (s *Service) ProcessingPriced(kind mediagen.Kind) bool {
	return !kind.Processing() || s.d.Charges.Priced(kind)
}
