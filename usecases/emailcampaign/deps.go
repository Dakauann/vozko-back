package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emailhygiene"
	"vozko/domain/emailsending"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	"vozko/domain/selection"
	balance_usecase "vozko/usecases/balance"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignqueue"
)

type Actor = conversation.Viewer

var QueueNamespace = campaign.Namespace{
	Topic: "email_campaign_dispatch",
	Key:   "campaign:email",
}

var (
	ErrIncomplete         = errors.New("emailcampaign: a required dependency is missing")
	ErrPublicBaseURL      = errors.New("emailcampaign: the public base url must be an https url")
	ErrTestRecipients     = errors.New("emailcampaign: a test goes to between 1 and 5 addresses")
	ErrTestAddressInvalid = errors.New("emailcampaign: a test address is invalid")
	ErrDaysInvalid        = errors.New("emailcampaign: the overview covers 1 to 90 days")
	ErrSenderUnavailable  = errors.New("emailcampaign: the campaign sender is not ready")
)

type LaunchBlockedError struct {
	Report emailcampaign.LaunchReport
}

func (e *LaunchBlockedError) Error() string {
	codes := make([]string, 0, len(e.Report.Blocking))
	for _, issue := range e.Report.Blocking {
		codes = append(codes, string(issue.Code))
	}
	return "emailcampaign: the launch is blocked: " + strings.Join(codes, ", ")
}

type CampaignBilling interface {
	BatchBilling
	ChargeMessage(ctx context.Context, workspaceID, messageKey string) (balance_usecase.EmailQuote, error)
	RefundMessage(ctx context.Context, workspaceID, messageKey string, charge balance_usecase.EmailQuote) error
}

type CampaignGateway interface {
	emailsending.Gateway
	emailsending.EnvelopeReader
}

type Wallet interface {
	GetByWorkspaceID(workspaceID string) (*balance.Balance, error)
}

type SenderStore interface {
	Get(ctx context.Context, workspaceID, id string) (*emailsending.Sender, error)
	GetDefault(ctx context.Context, workspaceID string) (*emailsending.Sender, error)
}

type DomainStore interface {
	Get(ctx context.Context, workspaceID, id string) (*emailsending.SendingDomain, error)
}

type SenderReactivation interface {
	Reactivate(ctx context.Context, workspaceID string) error
}

type AccountStore interface {
	GetByWorkspace(ctx context.Context, workspaceID string) (*emailsending.SenderAccount, error)
}

type HygieneReader interface {
	Standing(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]emailhygiene.Standing, error)
	Facts(address emailaddress.Address) emailhygiene.Facts
	RequestStale(ctx context.Context, standing map[emailaddress.Address]emailhygiene.Standing) error
}

type LeadFinder interface {
	FindByEmail(workspaceID, email string) (*lead.Lead, error)
}

type Deps struct {
	Campaigns     emailcampaign.Repository
	Recipients    emailcampaign.RecipientRepository
	Batches       emailcampaign.BillingStore
	Locks         cache.Locker
	Metrics       emailcampaign.MetricsReader
	Designs       emailcampaign.DesignRepository
	Selection     selection.Resolver
	LeadFacts     lead.EmailFactsReader
	Leads         LeadFinder
	Subscriptions emailconsent.Repository
	Hygiene       HygieneReader
	Outcomes      emailsending.RunOutcomeReader
	Gateway       CampaignGateway
	Senders       SenderStore
	Domains       DomainStore
	Accounts      AccountStore
	Reactivation  SenderReactivation
	Billing       CampaignBilling
	Wallet        Wallet
	Rates         balance_usecase.ExchangeRateReader
	QueuePub      messaging.MessageQueuePub
	QueueSub      messaging.MessageQueueSub
	Shared        cache.SharedState
	Memo          cache.Memo
	Notifications notification.EpisodePublisher
	PublicBaseURL string
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

type Service struct {
	deps       Deps
	audience   *campaignaudience_usecase.Service
	runner     *campaignqueue.Runner
	billing    *RunBilling
	dispatcher *campaignqueue.Dispatcher
	previewURL string
}

func New(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"campaigns": deps.Campaigns == nil, "recipients": deps.Recipients == nil, "batches": deps.Batches == nil, "locks": deps.Locks == nil, "metrics": deps.Metrics == nil,
		"designs": deps.Designs == nil, "selection": deps.Selection == nil, "lead facts": deps.LeadFacts == nil,
		"leads": deps.Leads == nil, "subscriptions": deps.Subscriptions == nil, "hygiene": deps.Hygiene == nil, "outcomes": deps.Outcomes == nil,
		"gateway": deps.Gateway == nil, "senders": deps.Senders == nil, "domains": deps.Domains == nil, "accounts": deps.Accounts == nil, "reactivation": deps.Reactivation == nil,
		"billing": deps.Billing == nil, "wallet": deps.Wallet == nil, "rates": deps.Rates == nil, "queue pub": deps.QueuePub == nil,
		"queue sub": deps.QueueSub == nil, "shared state": deps.Shared == nil, "memo": deps.Memo == nil,
		"notifications": deps.Notifications == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", ErrIncomplete, name)
		}
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(deps.PublicBaseURL), "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, ErrPublicBaseURL
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logf == nil {
		deps.Logf = log.Printf
	}
	s := &Service{deps: deps, previewURL: base.String() + PreviewPath}
	s.billing = NewRunBilling(RunBillingDeps{
		Campaigns: deps.Campaigns, Store: deps.Batches, Recipients: deps.Recipients, Billing: deps.Billing, Locks: deps.Locks,
		Now: s.now, Logf: s.logf,
	})
	s.dispatcher = campaignqueue.NewDispatcher(deps.QueuePub, deps.Shared, QueueNamespace)
	s.runner = campaignqueue.New(deps.QueueSub, deps.QueuePub, deps.Shared, statusStore{s: s}, pendingCounter{s: s},
		campaignqueue.Config{
			Namespace:         QueueNamespace,
			PauseRequeueDelay: pauseRequeueDelay,
			Logf: func(format string, args ...any) {
				deps.Logf("[email-campaign] "+format, args...)
			},
		}, s.handle)
	return s, nil
}

const PreviewPath = "/public/email/preview"

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) logf(format string, args ...any) {
	s.deps.Logf("[email-campaign] "+format, args...)
}

func (s *Service) StartConsumer() error {
	return s.runner.Start()
}

func scopeOf(a Actor) selection.Scope {
	return selection.Scope{WorkspaceID: a.WorkspaceID, ActorID: a.UserID, IsAdmin: a.IsAdmin}
}
