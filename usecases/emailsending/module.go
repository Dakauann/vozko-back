package emailsending

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/notification"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type OpsAlerter interface {
	Alert(ctx context.Context, subject, detail string) error
}

type TestBatchFailures interface {
	FailedTestBatches(ctx context.Context, workspaceID string, since time.Time) (int, error)
}

type AddressReputation interface {
	Record(ctx context.Context, m es.Message, ev es.DeliveryEvent, effect es.EventEffect) (es.SuppressReason, error)
}

type WorkspaceNames interface {
	WorkspaceName(ctx context.Context, workspaceID string) (string, error)
	WorkspaceNamesByID(ctx context.Context, workspaceIDs []string) (map[string]string, error)
}

type RecipientHygiene interface {
	SendRefusal(ctx context.Context, workspaceID string, kind es.RunKind, to emailaddress.Address) error
}

type Config struct {
	ProbationDomain string
	TrustedDomain   string
	PublicBaseURL   string
}

type Deps struct {
	Accounts     es.AccountRepository
	Domains      es.DomainRepository
	Senders      es.SenderRepository
	Usage        es.DailyUsage
	Suppressions es.SuppressionRepository
	Messages     es.MessageRepository
	Provider     es.Provider
	DNS          es.SubdomainDNS
	DMARC        es.DMARCLookup
	Records      es.DNSRecordLookup
	Monitor      es.DeliverabilityMonitor
	Decoder      es.NotificationDecoder
	Alerts       notification.EpisodePublisher
	Ops          OpsAlerter
	Tenants      es.TenantLifecycle
	Activity     es.SendActivity
	Campaigns    LiveCampaigns
	Locks        cache.Locker
	Routes       es.InboundRoutes
	ReplyPauses  es.ReplyPauses
	Traffic      SenderTraffic
	Shared       SendState
	Pacer        es.SendPacer
	Quota        es.SendQuotaReader
	Workspaces   WorkspaceNames
	Codec        *emailtoken.Codec
	SetupWake    es.SetupWake
	Reputation   AddressReputation
	Hygiene      RecipientHygiene
	TestBatches  TestBatchFailures
	Plans        workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	Unused       es.UnusedAccounts
	Config       Config
	Now          func() time.Time
	NewID        func() string
}

type Module struct {
	Setup          *Setup
	Domains        *Domains
	Senders        *Senders
	Suppressions   *Suppressions
	Gateway        *Gateway
	Events         *Events
	Promotion      *Promotion
	Admin          *Admin
	Deliverability *Deliverability
	Verification   *VerificationSweep
	Dormancy       *Dormancy
	UnusedRelease  *UnusedRelease
	Reactivation   *Reactivation
	Replies        *Replies
	ReplySweep     *ReplySweep
	SendQuota      *SendQuotaRefresh
	Capacity       *CapacityCheck
	HealthSweep    *TenantHealthSweep
}

type core struct {
	Deps
}

func New(d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	probation, _ := emailaddress.NormalizeDomain(d.Config.ProbationDomain)
	trusted, _ := emailaddress.NormalizeDomain(d.Config.TrustedDomain)
	d.Config.ProbationDomain, d.Config.TrustedDomain = probation, trusted
	d.Config.PublicBaseURL = strings.TrimRight(d.Config.PublicBaseURL, "/")
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	c := &core{Deps: d}
	return &Module{
		Setup:          &Setup{c: c},
		Domains:        &Domains{c: c},
		Senders:        &Senders{c: c},
		Suppressions:   &Suppressions{c: c},
		Gateway:        &Gateway{c: c},
		Events:         &Events{c: c},
		Promotion:      &Promotion{c: c},
		Admin:          &Admin{c: c},
		Deliverability: &Deliverability{c: c},
		Verification:   &VerificationSweep{c: c},
		Dormancy:       &Dormancy{c: c},
		UnusedRelease:  &UnusedRelease{c: c},
		Reactivation:   &Reactivation{c: c},
		Replies:        &Replies{c: c},
		ReplySweep:     &ReplySweep{c: c},
		SendQuota:      &SendQuotaRefresh{c: c},
		Capacity:       &CapacityCheck{c: c},
		HealthSweep:    &TenantHealthSweep{c: c},
	}, nil
}

func (d Deps) validate() error {
	missing := []string{}
	check := func(name string, ok bool) {
		if !ok {
			missing = append(missing, name)
		}
	}
	check("accounts", d.Accounts != nil)
	check("domains", d.Domains != nil)
	check("senders", d.Senders != nil)
	check("usage", d.Usage != nil)
	check("suppressions", d.Suppressions != nil)
	check("messages", d.Messages != nil)
	check("provider", d.Provider != nil)
	check("dns", d.DNS != nil)
	check("dmarc", d.DMARC != nil)
	check("record lookup", d.Records != nil)
	check("monitor", d.Monitor != nil)
	check("decoder", d.Decoder != nil)
	check("alerts", d.Alerts != nil)
	check("ops", d.Ops != nil)
	check("tenant lifecycle", d.Tenants != nil)
	check("send activity", d.Activity != nil)
	check("live campaigns", d.Campaigns != nil)
	check("locks", d.Locks != nil)
	check("inbound routes", d.Routes != nil)
	check("reply pauses", d.ReplyPauses != nil)
	check("sender traffic", d.Traffic != nil)
	check("shared send state", d.Shared != nil)
	check("send pacer", d.Pacer != nil)
	check("send quota", d.Quota != nil)
	check("workspaces", d.Workspaces != nil)
	check("codec", d.Codec != nil)
	check("setup wake", d.SetupWake != nil)
	check("reputation", d.Reputation != nil)
	check("recipient hygiene", d.Hygiene != nil)
	check("test batches", d.TestBatches != nil)
	check("plans", d.Plans != nil)
	check("unused accounts", d.Unused != nil)
	if len(missing) > 0 {
		return fmt.Errorf("emailsending: missing dependencies: %s", strings.Join(missing, ", "))
	}
	probation, err := emailaddress.NormalizeDomain(d.Config.ProbationDomain)
	if err != nil {
		return fmt.Errorf("emailsending: probation domain: %w", err)
	}
	trusted, err := emailaddress.NormalizeDomain(d.Config.TrustedDomain)
	if err != nil {
		return fmt.Errorf("emailsending: trusted domain: %w", err)
	}
	if nested(probation, trusted) {
		return errors.New("emailsending: the shared domains must be separate")
	}
	u, err := url.Parse(d.Config.PublicBaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("emailsending: the public base url must be an absolute https url")
	}
	return nil
}

func nested(a, b string) bool {
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

func (c *core) setupAllowed(workspaceID string) error {
	return es.SetupAllowedBy(c.Plans.Execute(workspaceID))
}

func (c *core) sharedDomainFor(kind es.DomainKind) string {
	if kind == es.DomainSharedTrusted {
		return c.Config.TrustedDomain
	}
	return c.Config.ProbationDomain
}

func (c *core) isSharedDomain(domain string) bool {
	return nested(domain, c.Config.ProbationDomain) || nested(domain, c.Config.TrustedDomain)
}

func (c *core) account(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	acct, err := c.Accounts.GetByWorkspace(ctx, workspaceID)
	if errors.Is(err, es.ErrAccountNotFound) {
		return nil, es.ErrAccountNotProvisioned
	}
	return acct, err
}

func (c *core) domainOfKind(ctx context.Context, workspaceID string, kind es.DomainKind) (*es.SendingDomain, error) {
	domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if domains[i].Kind == kind && !domains[i].Retired {
			return &domains[i], nil
		}
	}
	return nil, es.ErrDomainNotFound
}

func providerFailure(step string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrProviderUnavailable, step, err)
}
