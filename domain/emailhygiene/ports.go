package emailhygiene

import (
	"context"
	"time"

	"vozko/domain/emailaddress"
)

const DomainCheckTopic = "email_hygiene.domain_check"

type MailDomainResolver interface {
	Resolve(ctx context.Context, domain string) DNSAnswer
}

type DomainResult struct {
	Domain   string
	Verdict  DomainVerdict
	Attempts int
	Settled  Settlement
}

type DomainCheckStore interface {
	Read(ctx context.Context, domains []string) (map[string]DomainCheck, error)
	Request(ctx context.Context, domains []string, now time.Time) (int64, error)
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]DomainCheck, error)
	Save(ctx context.Context, results []DomainResult, now time.Time) error
	Prune(ctx context.Context, idleBefore time.Time, limit int) (int64, error)
}

type StandingReader interface {
	Standing(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]Standing, error)
}

type AddressSeen interface {
	AddressesSeen(ctx context.Context, addresses []emailaddress.Address)
}

type BadAddressStore interface {
	Add(ctx context.Context, hash AddressHash, at time.Time) error
}

type SoftStreak struct {
	Count     int
	StartedAt time.Time
}

type ActivityStore interface {
	Delivered(ctx context.Context, workspaceID string, hash AddressHash, messageID string, campaign bool, at time.Time) error
	SoftBounced(ctx context.Context, workspaceID string, hash AddressHash, messageID string, at time.Time) (SoftStreak, error)
	Engaged(ctx context.Context, workspaceID string, hash AddressHash, at time.Time) error
	Prune(ctx context.Context, touchedBefore time.Time, limit int) (int64, error)
}

type StoredList struct {
	Content   string
	FetchedAt time.Time
}

type DisposableListStore interface {
	Latest(ctx context.Context) (*StoredList, error)
	Save(ctx context.Context, content string, domains int, at time.Time) error
	KeepNewest(ctx context.Context, keep int) (int64, error)
}

type DisposableSource interface {
	Fetch(ctx context.Context) (string, error)
}

type DomainCheckKick interface {
	Kick() error
}
