package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	ads "vozko/domain/advertising"
)

const (
	staleJobAge     = 15 * time.Minute
	abandonedJobAge = 24 * time.Hour
	resumeBatchSize = 50
)

var ErrFeeNotCharged = errors.New("ads: the publishing fee could not be charged")

type publishGateway interface {
	mediaGateway
	preflightGateway
	CreateCampaign(ctx context.Context, token, metaAccountID string, spec ads.CampaignSpec) (string, error)
	CreateAdSet(ctx context.Context, token, metaAccountID string, spec ads.AdSetSpec) (string, error)
	CreateCreative(ctx context.Context, token, metaAccountID string, spec ads.CreativeSpec) (string, error)
	CreateAd(ctx context.Context, token, metaAccountID string, spec ads.AdSpec) (string, error)
	SetStatus(ctx context.Context, token, metaID string, status ads.ConfiguredStatus) error
	DeleteObject(ctx context.Context, token, metaID string) error
}

type PublishUseCase struct {
	access    accountAccess
	gateway   publishGateway
	jobs      ads.PublishJobRepository
	fees      FeeCharger
	media     creativeMedia
	preflight preflighter
	sync      *SyncUseCase
}

func NewPublishUseCase(sync *SyncUseCase, gateway publishGateway, jobs ads.PublishJobRepository, numbers ads.NumberDirectory, source MediaSource, fees FeeCharger) *PublishUseCase {
	media := newCreativeMedia(source, gateway)
	return &PublishUseCase{
		access: sync.access, gateway: gateway, jobs: jobs, fees: fees, media: media, sync: sync,
		preflight: preflighter{access: sync.access, gateway: gateway, objects: sync.objects, numbers: numbers, media: media, fees: fees, floor: budgetFloor{access: sync.access, gateway: gateway}},
	}
}

type localFailure struct{ err error }

func (f *localFailure) Error() string { return f.err.Error() }
func (f *localFailure) Unwrap() error { return f.err }

func (uc *PublishUseCase) Preflight(ctx context.Context, workspaceID string, draft ads.AdDraft) (*Preflight, error) {
	return uc.preflight.run(ctx, workspaceID, draft)
}

type PublishInput struct {
	WorkspaceID string
	UserID      string
	Actor       ads.Actor
	Draft       ads.AdDraft
}

func (uc *PublishUseCase) Publish(ctx context.Context, in PublishInput) (*ads.PublishJob, error) {
	pre, err := uc.Preflight(ctx, in.WorkspaceID, in.Draft)
	if err != nil {
		return nil, err
	}
	job := &ads.PublishJob{
		WorkspaceID: in.WorkspaceID,
		AdAccountID: pre.Account.ID,
		CreatedBy:   in.UserID,
		Actor:       in.Actor,
		Draft:       pre.Draft,
		Status:      ads.JobQueued,
	}
	if err := uc.jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	fee, err := uc.fees.Charge(in.WorkspaceID, job.FeeReference(), job.AdsToPublish())
	if err != nil {
		job.Fail("fee_not_charged", err.Error())
		if saveErr := uc.jobs.Save(ctx, job); saveErr != nil {
			log.Printf("[ads] job %s could not record the fee failure: %v", job.ID, saveErr)
		}
		return job, fmt.Errorf("%w: %w", ErrFeeNotCharged, err)
	}
	job.Fee = ads.FeeCharged
	job.FeeMicros = fee.PriceMicros
	if err := uc.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	return uc.Run(ctx, job)
}

func (uc *PublishUseCase) Run(ctx context.Context, job *ads.PublishJob) (*ads.PublishJob, error) {
	claimed, err := uc.jobs.Claim(ctx, job.ID, ads.JobQueued, ads.JobRunning)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return uc.jobs.Find(ctx, job.WorkspaceID, job.ID)
	}
	job.Status = ads.JobRunning
	account, token, err := uc.access.open(ctx, job.WorkspaceID, job.AdAccountID, ads.UseWrite)
	if err == nil {
		err = account.CanSpend()
	}
	if err != nil {
		return uc.settle(ctx, job, nil, "", ads.Step{}, &localFailure{err: err})
	}
	for {
		step, pending := job.NextStep()
		if !pending {
			break
		}
		job.Begin(step)
		if err := uc.jobs.Save(ctx, job); err != nil {
			return nil, err
		}
		id, err := uc.perform(ctx, job, account, token, step)
		if err != nil {
			return uc.settle(ctx, job, account, token, step, err)
		}
		if err := job.Complete(step, id); err != nil {
			return uc.settle(ctx, job, account, token, step, err)
		}
		if err := uc.jobs.Save(ctx, job); err != nil {
			return nil, err
		}
	}
	job.Publish()
	if err := uc.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	if err := uc.sync.SyncStructure(ctx, account, token); err != nil {
		log.Printf("[ads] job %s published but the refresh failed: %v", job.ID, err)
	}
	return job, nil
}

func (uc *PublishUseCase) perform(ctx context.Context, job *ads.PublishJob, account *ads.AdAccount, token string, step ads.Step) (string, error) {
	d, act := job.Draft, account.MetaAccountID
	switch step.Kind {
	case ads.StepMedia:
		return uc.media.upload(ctx, job.WorkspaceID, account, token, step.Media)
	case ads.StepVideoReady:
		return "", uc.media.waitReady(ctx, token, job.Progress.Media[step.Media.MediaID])
	case ads.StepCampaign:
		return uc.gateway.CreateCampaign(ctx, token, act, ads.CampaignSpecOf(d))
	case ads.StepAdSet:
		return uc.gateway.CreateAdSet(ctx, token, act, ads.AdSetSpecOf(d, job.CampaignID()))
	case ads.StepCreative:
		return uc.gateway.CreateCreative(ctx, token, act, ads.CreativeSpecOf(d, step.Index, job.UploadedMedia()))
	case ads.StepAd:
		return uc.gateway.CreateAd(ctx, token, act, ads.AdSpecOf(d, step.Index, job.AdSetID(), job.Progress.Creatives[step.Index], job.UploadedMedia()))
	case ads.StepActivate:
		return "", uc.switchOn(ctx, token, job)
	}
	return "", &localFailure{err: fmt.Errorf("ads: unknown publish step %q", step.Kind)}
}

func (uc *PublishUseCase) settle(ctx context.Context, job *ads.PublishJob, account *ads.AdAccount, token string, step ads.Step, cause error) (*ads.PublishJob, error) {
	var local *localFailure
	failure := ads.Classify(cause)
	switch {
	case errors.As(cause, &local):
		job.Release()
		job.Fail("not_publishable", local.Error())
	case failure == ads.FailureRetryable || (failure == ads.FailureUnknown && step.RetrySafe()):
		job.Release()
		job.Status = ads.JobQueued
		job.ErrorCode, job.ErrorMessage = "retrying", cause.Error()
		return job, uc.jobs.Save(ctx, job)
	case failure == ads.FailureUnknown:
		job.Review("ambiguous", "a Meta não confirmou a etapa "+step.Key()+": "+cause.Error())
		log.Printf("[ads] job %s needs review after %s: %v", job.ID, step.Key(), cause)
		return job, uc.jobs.Save(ctx, job)
	default:
		if account != nil {
			uc.access.failed(ctx, account, cause)
		}
		job.Release()
		message := ads.Explain(cause)
		if message == "" {
			message = cause.Error()
		}
		job.Fail(ads.FailureCode(cause), message)
	}
	if account != nil && !job.Progress.Activated {
		uc.cleanup(ctx, job, token)
	}
	if job.RefundDue() {
		if err := uc.fees.Refund(job.WorkspaceID, job.FeeReference(), job.FeeMicros); err != nil {
			log.Printf("[ads] job %s failed but its fee could not be refunded: %v", job.ID, err)
		} else {
			job.Fee = ads.FeeRefunded
		}
	}
	return job, uc.jobs.Save(ctx, job)
}

func (uc *PublishUseCase) cleanup(ctx context.Context, job *ads.PublishJob, token string) {
	for _, id := range job.CreatedObjects() {
		if err := uc.gateway.DeleteObject(ctx, token, id); err != nil {
			log.Printf("[ads] job %s left paused object %s behind: %v", job.ID, id, err)
		}
	}
}

func (uc *PublishUseCase) Resume(ctx context.Context) error {
	now := uc.access.now()
	stale, err := uc.jobs.ListStale(ctx, now.Add(-staleJobAge), resumeBatchSize)
	if err != nil {
		return err
	}
	for _, job := range stale {
		if err := uc.resume(ctx, job, now); err != nil {
			log.Printf("[ads] resuming job %s failed: %v", job.ID, err)
		}
	}
	return nil
}

func (uc *PublishUseCase) resume(ctx context.Context, job *ads.PublishJob, now time.Time) error {
	if now.Sub(job.CreatedAt) > abandonedJobAge {
		job.Review("stalled", "a publicação ficou parada por mais de 24 horas; confira no Gerenciador de Anúncios antes de tentar de novo")
		return uc.jobs.Save(ctx, job)
	}
	if job.Status == ads.JobRunning {
		if job.Interrupted() {
			job.Review("interrupted", "o servidor parou enquanto a Meta criava a etapa "+job.Progress.InFlight+"; confira no Gerenciador de Anúncios antes de tentar de novo")
			return uc.jobs.Save(ctx, job)
		}
		claimed, err := uc.jobs.Claim(ctx, job.ID, ads.JobRunning, ads.JobQueued)
		if err != nil || !claimed {
			return err
		}
		job.Status = ads.JobQueued
		job.Release()
	}
	_, err := uc.Run(ctx, job)
	return err
}

func (uc *PublishUseCase) Jobs(ctx context.Context, workspaceID string, limit int) ([]*ads.PublishJob, error) {
	return uc.jobs.ListByWorkspace(ctx, workspaceID, limit)
}

func (uc *PublishUseCase) switchOn(ctx context.Context, token string, job *ads.PublishJob) error {
	for _, id := range job.ToActivate() {
		if err := uc.gateway.SetStatus(ctx, token, id, ads.StatusActive); err != nil {
			return err
		}
	}
	return nil
}

func (uc *PublishUseCase) SwitchOn(ctx context.Context, workspaceID, id string) (*ads.PublishJob, error) {
	job, err := uc.jobs.Find(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := job.CanSwitchOnLater(); err != nil {
		return nil, err
	}
	account, token, err := uc.access.open(ctx, workspaceID, job.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, err
	}
	if err := account.CanSpend(); err != nil {
		return nil, err
	}
	if err := uc.switchOn(ctx, token, job); err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	job.SwitchedOn()
	if err := uc.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	if err := uc.sync.SyncStructure(ctx, account, token); err != nil {
		log.Printf("[ads] job %s switched on but the refresh failed: %v", job.ID, err)
	}
	return job, nil
}

func (uc *PublishUseCase) Job(ctx context.Context, workspaceID, id string) (*ads.PublishJob, error) {
	return uc.jobs.Find(ctx, workspaceID, id)
}
