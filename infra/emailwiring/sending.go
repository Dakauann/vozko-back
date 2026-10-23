package emailwiring

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/cache"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	emailsending_repository "vozko/infra/repositories/emailsending"
	esuc "vozko/usecases/emailsending"
	"vozko/usecases/shared/delayedfire"
)

const (
	TenantLockTTL  = 5 * time.Minute
	TenantLockPoll = 200 * time.Millisecond
)

type SendingAdapters struct {
	Provider es.Provider
	DNS      es.SubdomainDNS
	DMARC    es.DMARCLookup
	Records  es.DNSRecordLookup
	Monitor  es.DeliverabilityMonitor
	Decoder  es.NotificationDecoder
	Alerts   notification.EpisodePublisher
	Ops      esuc.OpsAlerter
	Tenants  es.TenantLifecycle
	Routes   es.InboundRoutes
	Quota    es.SendQuotaReader
	Shared   cache.SharedState
	QueuePub messaging.MessageQueuePub
	Codec    *emailtoken.Codec
	Hygiene  esuc.RecipientHygiene
	Plans    workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	Config   esuc.Config
}

func NewSendingModule(db *gorm.DB, a SendingAdapters) (*esuc.Module, error) {
	if db == nil || a.Shared == nil || a.QueuePub == nil || a.Hygiene == nil {
		return nil, fmt.Errorf("%w: the database, the shared state, the queue publisher and recipient hygiene", ErrIncomplete)
	}
	setupWake, err := delayedfire.NewScheduler(a.QueuePub, es.SetupFireTopic)
	if err != nil {
		return nil, fmt.Errorf("emailwiring: setup wake: %w", err)
	}
	pacer, err := esuc.NewSendPacer(esuc.PacerDeps{State: a.Shared, Ops: a.Ops})
	if err != nil {
		return nil, fmt.Errorf("emailwiring: SES send pacer: %w", err)
	}
	locks, err := cache.NewOwnedLock(a.Shared, TenantLockTTL, TenantLockPoll)
	if err != nil {
		return nil, fmt.Errorf("emailwiring: tenant lock: %w", err)
	}
	return esuc.New(esuc.Deps{
		Accounts:     emailsending_repository.NewAccountRepository(db),
		Domains:      emailsending_repository.NewDomainRepository(db),
		Senders:      emailsending_repository.NewSenderRepository(db),
		Usage:        emailsending_repository.NewDailyUsageRepository(db),
		Suppressions: emailsending_repository.NewSuppressionRepository(db),
		Messages:     emailsending_repository.NewMessageRepository(db),
		Provider:     a.Provider,
		DNS:          a.DNS,
		DMARC:        a.DMARC,
		Records:      a.Records,
		Monitor:      a.Monitor,
		Decoder:      a.Decoder,
		Alerts:       a.Alerts,
		Ops:          a.Ops,
		Tenants:      a.Tenants,
		Activity:     emailsending_repository.NewSendActivity(db),
		Campaigns:    emailcampaign_repository.NewLiveCampaigns(db),
		Locks:        locks,
		Routes:       a.Routes,
		ReplyPauses:  emailsending_repository.NewReplyPauseRepository(db),
		Traffic:      a.Shared,
		Shared:       a.Shared,
		Pacer:        pacer,
		Quota:        a.Quota,
		Workspaces:   emailsending_repository.NewWorkspaceNames(db),
		Codec:        a.Codec,
		SetupWake:    setupWake,
		Reputation:   NewReputation(db),
		Hygiene:      a.Hygiene,
		TestBatches:  emailcampaign_repository.NewCampaignRepository(db),
		Plans:        a.Plans,
		Unused:       emailsending_repository.NewUnusedAccounts(db),
		Config:       a.Config,
	})
}
