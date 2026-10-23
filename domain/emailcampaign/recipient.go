package emailcampaign

import (
	"errors"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
)

var (
	ErrRecipientNotFound      = errors.New("emailcampaign: recipient not found")
	ErrRecipientFilterInvalid = errors.New("emailcampaign: unknown recipient filter")
)

type RecipientStatus string

const (
	RecipientPending RecipientStatus = "pending"
	RecipientSkipped RecipientStatus = "skipped"
	RecipientSent    RecipientStatus = "sent"
	RecipientFailed  RecipientStatus = "failed"
)

func (s RecipientStatus) Valid() bool {
	switch s {
	case RecipientPending, RecipientSkipped, RecipientSent, RecipientFailed:
		return true
	}
	return false
}

type SkipReason string

const (
	SkipNone            SkipReason = ""
	SkipNoEmail         SkipReason = "no_email"
	SkipInvalidAddress  SkipReason = "invalid_address"
	SkipDuplicate       SkipReason = "duplicate"
	SkipOptedOut        SkipReason = "opted_out"
	SkipBlocked         SkipReason = "blocked"
	SkipUnsubscribed    SkipReason = "unsubscribed"
	SkipSuppressed      SkipReason = "suppressed"
	SkipMissingVariable SkipReason = "missing_variable"
	SkipBadAddress      SkipReason = "bad_address"
	SkipNoMailDomain    SkipReason = "no_mail_domain"
	SkipDomainUnknown   SkipReason = "domain_unknown"
	SkipDomainChecking  SkipReason = "domain_checking"
	SkipDisposable      SkipReason = "disposable"
	SkipInactive        SkipReason = "inactive"
)

func AllSkipReasons() []SkipReason {
	return []SkipReason{SkipNoEmail, SkipInvalidAddress, SkipDuplicate, SkipOptedOut, SkipBlocked, SkipUnsubscribed, SkipSuppressed, SkipMissingVariable,
		SkipBadAddress, SkipNoMailDomain, SkipDomainUnknown, SkipDomainChecking, SkipDisposable, SkipInactive}
}

func (r SkipReason) Valid() bool {
	for _, known := range AllSkipReasons() {
		if r == known {
			return true
		}
	}
	return false
}

func (r SkipReason) KeepsAddressUnique() bool {
	return r != SkipNoEmail && r != SkipInvalidAddress && r != SkipDuplicate
}

func SkipFromConsent(r emailconsent.SkipReason) SkipReason {
	switch r {
	case emailconsent.SkipUnsubscribed:
		return SkipUnsubscribed
	case emailconsent.SkipOptedOut:
		return SkipOptedOut
	case emailconsent.SkipBlocked:
		return SkipBlocked
	}
	return SkipNone
}

type Recipient struct {
	ID          string
	WorkspaceID string
	CampaignID  string
	LeadID      string
	Address     emailaddress.Address
	Variant     Variant
	Status      RecipientStatus
	SkipReason  SkipReason
	MessageID   string
	FailureCode string
	SentAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type RecipientCounts struct {
	Total   int64
	Pending int64
	Sent    int64
	Failed  int64
	Skipped map[SkipReason]int64
}

func (c RecipientCounts) SkippedTotal() int64 {
	var n int64
	for _, v := range c.Skipped {
		n += v
	}
	return n
}

type RecipientFilter struct {
	Status     RecipientStatus
	SkipReason SkipReason
	Variant    Variant
	Search     string
}

type RecipientRow struct {
	Recipient
	LeadName      string
	MessageStatus string
	DeliveredAt   *time.Time
	FirstOpenAt   *time.Time
	FirstClickAt  *time.Time
	BouncedAt     *time.Time
	BounceKind    string
	ComplainedAt  *time.Time
	Unsubscribed  *time.Time
}
