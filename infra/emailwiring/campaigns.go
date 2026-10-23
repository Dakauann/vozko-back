package emailwiring

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/cache"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	"vozko/domain/selection"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	emailsending_repository "vozko/infra/repositories/emailsending"
	lead_repository "vozko/infra/repositories/lead"
	balance_usecase "vozko/usecases/balance"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	emailconsent_usecase "vozko/usecases/emailconsent"
	esuc "vozko/usecases/emailsending"
)

const (
	CampaignBillingLockTTL  = 30 * time.Second
	CampaignBillingLockPoll = 50 * time.Millisecond
)

type Leads interface {
	FindByEmail(workspaceID, email string) (*lead.Lead, error)
}

type CampaignAdapters struct {
	Sending       *esuc.Module
	Codec         *emailtoken.Codec
	Billing       emailcampaign_usecase.CampaignBilling
	Selection     selection.Resolver
	Leads         Leads
	Wallet        emailcampaign_usecase.Wallet
	Rates         balance_usecase.ExchangeRateReader
	QueuePub      messaging.MessageQueuePub
	QueueSub      messaging.MessageQueueSub
	Shared        cache.SharedState
	Memo          cache.Memo
	Notifications notification.EpisodePublisher
	PublicBaseURL string
	Audience      *campaignaudience_usecase.Service
	Hygiene       emailcampaign_usecase.HygieneReader
}

type Campaigns struct {
	Service *emailcampaign_usecase.Service
	Consent *emailconsent_usecase.Service
}

func NewCampaigns(db *gorm.DB, a CampaignAdapters) (*Campaigns, error) {
	if db == nil || a.Sending == nil || a.Sending.Events == nil || a.Codec == nil || a.Leads == nil || a.Audience == nil || a.Hygiene == nil {
		return nil, fmt.Errorf("%w: the database, email sending, the token codec, the leads, the audience step and hygiene", ErrIncomplete)
	}
	subscriptions := emailcampaign_repository.NewSubscriptionRepository(db)
	consent, err := emailconsent_usecase.New(emailconsent_usecase.Deps{
		Subscriptions: subscriptions,
		Messages:      emailsending_repository.NewMessageRepository(db),
		Tokens:        a.Codec,
		Leads:         a.Leads,
	})
	if err != nil {
		return nil, fmt.Errorf("emailwiring: consent: %w", err)
	}
	a.Sending.Events.SetUnsubscriber(consent)
	locks, err := cache.NewOwnedLock(a.Shared, CampaignBillingLockTTL, CampaignBillingLockPoll)
	if err != nil {
		return nil, fmt.Errorf("emailwiring: campaign billing lock: %w", err)
	}
	service, err := emailcampaign_usecase.New(emailcampaign_usecase.Deps{
		Campaigns:     emailcampaign_repository.NewCampaignRepository(db),
		Recipients:    emailcampaign_repository.NewRecipientRepository(db),
		Batches:       emailcampaign_repository.NewBillingRepository(db),
		Locks:         locks,
		Metrics:       emailcampaign_repository.NewMetricsRepository(db),
		Designs:       emailcampaign_repository.NewDesignRepository(db),
		Selection:     a.Selection,
		LeadFacts:     lead_repository.NewEmailFacts(db),
		Leads:         a.Leads,
		Subscriptions: subscriptions,
		Hygiene:       a.Hygiene,
		Outcomes:      emailsending_repository.NewRunOutcomes(db),
		Gateway:       a.Sending.Gateway,
		Senders:       emailsending_repository.NewSenderRepository(db),
		Domains:       emailsending_repository.NewDomainRepository(db),
		Accounts:      emailsending_repository.NewAccountRepository(db),
		Reactivation:  a.Sending.Reactivation,
		Billing:       a.Billing,
		Wallet:        a.Wallet,
		Rates:         a.Rates,
		QueuePub:      a.QueuePub,
		QueueSub:      a.QueueSub,
		Shared:        a.Shared,
		Memo:          a.Memo,
		Notifications: a.Notifications,
		PublicBaseURL: a.PublicBaseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("emailwiring: campaigns: %w", err)
	}
	a.Audience.AddChannel(service.AudienceChannel())
	service.SetAudience(a.Audience)
	return &Campaigns{Service: service, Consent: consent}, nil
}
