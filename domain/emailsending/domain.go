package emailsending

import "time"

type DomainKind string

const (
	DomainSharedProbation DomainKind = "shared_probation"
	DomainSharedTrusted   DomainKind = "shared_trusted"
	DomainOwn             DomainKind = "own"
)

func (k DomainKind) Valid() bool {
	switch k {
	case DomainSharedProbation, DomainSharedTrusted, DomainOwn:
		return true
	}
	return false
}

func (k DomainKind) Shared() bool {
	return k == DomainSharedProbation || k == DomainSharedTrusted
}

type DomainStatus string

const (
	DomainPending   DomainStatus = "pending"
	DomainVerifying DomainStatus = "verifying"
	DomainVerified  DomainStatus = "verified"
	DomainFailed    DomainStatus = "failed"
)

type ConnectMethod string

const (
	ConnectAutomatic ConnectMethod = "automatic"
	ConnectManual    ConnectMethod = "manual"
)

func (m ConnectMethod) Valid() bool {
	switch m {
	case ConnectAutomatic, ConnectManual:
		return true
	}
	return false
}

type SendingDomain struct {
	ID              string
	WorkspaceID     string
	AccountID       string
	Domain          string
	Kind            DomainKind
	Status          DomainStatus
	ConnectMethod   ConnectMethod
	MailFromDomain  string
	ReplyDomain     string
	Records         []DNSRecord
	FailureReason   string
	SetupFailure    SetupFailure
	VerifiedAt      *time.Time
	LastCheckedAt   *time.Time
	VerifyingSince  *time.Time
	Retired         bool
	ReleaseState    ReleaseState
	RepliesPausedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (d SendingDomain) Sendable() bool {
	return d.Status == DomainVerified && !d.Retired && d.Kind.Valid()
}

type IdentityState struct {
	DKIMVerified     bool
	MailFromVerified bool
	Failed           bool
	FailureReason    string
}

func (s IdentityState) Verified() bool {
	return s.DKIMVerified && s.MailFromVerified && !s.Failed
}

func NextDomainStatus(current DomainStatus, state IdentityState) DomainStatus {
	switch {
	case state.Failed:
		return DomainFailed
	case state.Verified():
		return DomainVerified
	case current == DomainVerified:
		return DomainVerifying
	}
	return DomainVerifying
}
