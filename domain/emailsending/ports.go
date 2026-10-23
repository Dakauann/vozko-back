package emailsending

import (
	"context"
	"time"

	"vozko/domain/emailaddress"
)

type TenantSpec struct {
	Name string
}

type Tenant struct {
	Name string
	Ref  string
}

type IdentitySpec struct {
	TenantName     string
	Domain         string
	MailFromDomain string
}

type TenantHealth struct {
	Sending  TenantSendingStatus
	Findings []FindingEvent
}

type Provider interface {
	Name() string
	EnsureTenant(ctx context.Context, spec TenantSpec) (Tenant, error)
	EnsureIdentity(ctx context.Context, spec IdentitySpec) (IdentitySetup, error)
	IdentityStatus(ctx context.Context, domain string) (IdentityState, error)
	RemoveIdentity(ctx context.Context, tenantName, domain string) error
	Send(ctx context.Context, msg OutboundMessage) (SendReceipt, error)
	SetTenantSending(ctx context.Context, tenantName string, enabled bool) error
	TenantHealth(ctx context.Context, tenantName string) (TenantHealth, error)
	SuppressAddress(ctx context.Context, tenantName string, address emailaddress.Address, reason SuppressReason) error
	UnsuppressAddress(ctx context.Context, tenantName string, address emailaddress.Address) error
}

type NotificationDecoder interface {
	Decode(payload []byte) (Notification, error)
}

type SubdomainDNS interface {
	UpsertRecords(ctx context.Context, sharedDomain string, records []DNSRecord) error
	DeleteRecords(ctx context.Context, sharedDomain string, records []DNSRecord) error
}

type DMARCLookup interface {
	LookupDMARC(ctx context.Context, domain string) (string, error)
}

type DNSRecordLookup interface {
	LookupMX(ctx context.Context, name string) ([]string, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type DomainStats struct {
	Domain         string
	Day            string
	SpamRatePerMil int
	Available      bool
}

type DeliverabilityMonitor interface {
	Name() string
	SpamRate(ctx context.Context, domain string, day time.Time) (DomainStats, error)
}

type AccountRepository interface {
	GetByWorkspace(ctx context.Context, workspaceID string) (*SenderAccount, error)
	GetByTenantName(ctx context.Context, tenantName string) (*SenderAccount, error)
	Create(ctx context.Context, account *SenderAccount) error
	Update(ctx context.Context, account *SenderAccount) error
	SlugTaken(ctx context.Context, slug string) (bool, error)
	ChangeSlug(ctx context.Context, accountID, slug string) error
	ListByMode(ctx context.Context, mode Mode) ([]SenderAccount, error)
	ListByStatus(ctx context.Context, status AccountStatus) ([]SenderAccount, error)
	List(ctx context.Context, offset, limit int) ([]SenderAccount, int64, error)
}

type DomainRepository interface {
	Get(ctx context.Context, workspaceID, id string) (*SendingDomain, error)
	GetByDomain(ctx context.Context, domain string) (*SendingDomain, error)
	GetByReplyDomain(ctx context.Context, replyDomain string) (*SendingDomain, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]SendingDomain, error)
	ListPending(ctx context.Context, limit int) ([]SendingDomain, error)
	ListVerified(ctx context.Context, kind DomainKind, offset, limit int) ([]SendingDomain, error)
	Create(ctx context.Context, domain *SendingDomain) error
	Update(ctx context.Context, domain *SendingDomain) error
	SetRelease(ctx context.Context, domain *SendingDomain) error
}

type SenderRepository interface {
	Get(ctx context.Context, workspaceID, id string) (*Sender, error)
	GetDefault(ctx context.Context, workspaceID string) (*Sender, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Sender, error)
	Create(ctx context.Context, sender *Sender) error
	Update(ctx context.Context, sender *Sender) error
	Delete(ctx context.Context, workspaceID, id string) error
	MoveDomain(ctx context.Context, workspaceID, fromDomainID, toDomainID string) error
}

type DailyUsage interface {
	TakeDailySlot(ctx context.Context, domainID, day string, cap int64) (bool, error)
	GiveBackDailySlot(ctx context.Context, domainID, day string) error
	Used(ctx context.Context, domainID, day string) (int64, error)
}

type Suppression struct {
	WorkspaceID   string
	Address       emailaddress.Address
	Reason        SuppressReason
	SourceMessage string
	SuppressedAt  time.Time
}

type SuppressionRepository interface {
	Suppress(ctx context.Context, s Suppression) error
	Remove(ctx context.Context, workspaceID string, address emailaddress.Address) error
	Suppressed(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]SuppressReason, error)
	List(ctx context.Context, workspaceID string, q SuppressionQuery) ([]Suppression, int64, error)
}

type SuppressionQuery struct {
	Prefix string
	Offset int
	Limit  int
}

type MessageRepository interface {
	Record(ctx context.Context, m *Message) error
	Get(ctx context.Context, id string) (*Message, error)
	GetByProviderID(ctx context.Context, providerMessageID string) (*Message, error)
	Update(ctx context.Context, m *Message) error
	AppendEvent(ctx context.Context, messageID string, ev DeliveryEvent) (AppendedEvent, error)
	HealthWindow(ctx context.Context, workspaceID string, since time.Time) (HealthWindow, error)
}

type RunMessages interface {
	ListByRun(ctx context.Context, workspaceID string, kind RunKind, runID string) ([]Message, error)
}

type RunOutcome struct {
	Delivered   int64
	HardBounces int64
	Complaints  int64
}

func (e EventEffect) CountsTowardRunOutcome() bool {
	return e.Delivered || e.HardBounced || e.Complained
}

type RunOutcomeReader interface {
	RunOutcome(ctx context.Context, kind RunKind, runID string) (RunOutcome, error)
}
