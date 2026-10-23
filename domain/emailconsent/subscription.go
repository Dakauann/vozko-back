package emailconsent

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"vozko/domain/emailaddress"
)

var (
	ErrWorkspaceRequired        = errors.New("emailconsent: a workspace is required")
	ErrAddressInvalid           = errors.New("emailconsent: invalid email address")
	ErrSourceInvalid            = errors.New("emailconsent: unknown consent source")
	ErrConsentIPRequired        = errors.New("emailconsent: a form consent needs the ip it came from")
	ErrConsentIPInvalid         = errors.New("emailconsent: invalid consent ip")
	ErrConsentDateRequired      = errors.New("emailconsent: the consent date is required")
	ErrConsentDateInFuture      = errors.New("emailconsent: the consent date is in the future")
	ErrUnsubscribeSourceInvalid = errors.New("emailconsent: unknown unsubscribe source")
	ErrAddressMismatch          = errors.New("emailconsent: the consent belongs to another address")
	ErrSubscriptionNotFound     = errors.New("emailconsent: subscription not found")
)

const consentClockSkew = 10 * time.Minute

type Status string

const (
	StatusSubscribed   Status = "subscribed"
	StatusUnsubscribed Status = "unsubscribed"
)

type Source string

const (
	SourceImport   Source = "import"
	SourceForm     Source = "form"
	SourceOperator Source = "operator"
	SourceAPI      Source = "api"
)

func (s Source) Valid() bool {
	switch s {
	case SourceImport, SourceForm, SourceOperator, SourceAPI:
		return true
	}
	return false
}

type UnsubscribeSource string

const (
	UnsubscribeOneClick  UnsubscribeSource = "one_click"
	UnsubscribeLink      UnsubscribeSource = "link"
	UnsubscribeMailto    UnsubscribeSource = "mailto"
	UnsubscribeComplaint UnsubscribeSource = "complaint"
	UnsubscribeOperator  UnsubscribeSource = "operator"
)

func (s UnsubscribeSource) Valid() bool {
	switch s {
	case UnsubscribeOneClick, UnsubscribeLink, UnsubscribeMailto, UnsubscribeComplaint, UnsubscribeOperator:
		return true
	}
	return false
}

type Subscription struct {
	ID                string
	WorkspaceID       string
	Address           emailaddress.Address
	LeadID            string
	Status            Status
	Source            Source
	ConsentedAt       time.Time
	ConsentIP         string
	UnsubscribedAt    *time.Time
	UnsubscribeSource UnsubscribeSource
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Consent struct {
	WorkspaceID string
	Address     string
	LeadID      string
	Source      Source
	IP          string
	At          time.Time
}

func NewSubscription(c Consent, now time.Time) (Subscription, error) {
	workspaceID := strings.TrimSpace(c.WorkspaceID)
	if workspaceID == "" {
		return Subscription{}, ErrWorkspaceRequired
	}
	address, err := emailaddress.Normalize(c.Address)
	if err != nil {
		return Subscription{}, ErrAddressInvalid
	}
	if !c.Source.Valid() {
		return Subscription{}, ErrSourceInvalid
	}
	ip, err := consentIP(c.Source, c.IP)
	if err != nil {
		return Subscription{}, err
	}
	if c.At.IsZero() {
		return Subscription{}, ErrConsentDateRequired
	}
	if c.At.After(now.Add(consentClockSkew)) {
		return Subscription{}, ErrConsentDateInFuture
	}
	return Subscription{
		WorkspaceID: workspaceID,
		Address:     address,
		LeadID:      strings.TrimSpace(c.LeadID),
		Status:      StatusSubscribed,
		Source:      c.Source,
		ConsentedAt: c.At.UTC(),
		ConsentIP:   ip,
	}, nil
}

func NewUnsubscribed(workspaceID, rawAddress, leadID string, source UnsubscribeSource, at time.Time) (Subscription, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return Subscription{}, ErrWorkspaceRequired
	}
	address, err := emailaddress.Normalize(rawAddress)
	if err != nil {
		return Subscription{}, ErrAddressInvalid
	}
	if !source.Valid() {
		return Subscription{}, ErrUnsubscribeSourceInvalid
	}
	stamp := at.UTC()
	return Subscription{
		WorkspaceID:       workspaceID,
		Address:           address,
		LeadID:            strings.TrimSpace(leadID),
		Status:            StatusUnsubscribed,
		UnsubscribedAt:    &stamp,
		UnsubscribeSource: source,
	}, nil
}

func consentIP(source Source, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if source == SourceForm {
			return "", ErrConsentIPRequired
		}
		return "", nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", ErrConsentIPInvalid
	}
	return addr.Unmap().String(), nil
}

func (s Subscription) Renew(c Consent, now time.Time) (Subscription, bool, error) {
	fresh, err := NewSubscription(c, now)
	if err != nil {
		return s, false, err
	}
	if fresh.WorkspaceID != s.WorkspaceID || fresh.Address != s.Address {
		return s, false, ErrAddressMismatch
	}
	if s.Status == StatusUnsubscribed && fresh.Source == SourceImport {
		return s, false, nil
	}
	if s.Status == StatusSubscribed {
		if fresh.LeadID == "" || fresh.LeadID == s.LeadID {
			return s, false, nil
		}
		s.LeadID = fresh.LeadID
		return s, true, nil
	}
	fresh.ID, fresh.CreatedAt = s.ID, s.CreatedAt
	if fresh.LeadID == "" {
		fresh.LeadID = s.LeadID
	}
	return fresh, true, nil
}

func (s Subscription) Unsubscribe(source UnsubscribeSource, at time.Time) (Subscription, bool, error) {
	if !source.Valid() {
		return s, false, ErrUnsubscribeSourceInvalid
	}
	if s.Status == StatusUnsubscribed {
		return s, false, nil
	}
	stamp := at.UTC()
	s.Status = StatusUnsubscribed
	s.UnsubscribedAt = &stamp
	s.UnsubscribeSource = source
	return s, true, nil
}

type SkipReason string

const (
	SkipNone         SkipReason = ""
	SkipUnsubscribed SkipReason = "unsubscribed"
	SkipOptedOut     SkipReason = "opted_out"
	SkipBlocked      SkipReason = "blocked"
)

type LeadFacts struct {
	OptedOut bool
	Blocked  bool
}

func MarketingEligibility(sub *Subscription, facts LeadFacts) SkipReason {
	switch {
	case sub != nil && sub.Status != StatusSubscribed:
		return SkipUnsubscribed
	case facts.OptedOut:
		return SkipOptedOut
	case facts.Blocked:
		return SkipBlocked
	}
	return SkipNone
}

type Repository interface {
	Get(ctx context.Context, workspaceID string, address emailaddress.Address) (*Subscription, error)
	Find(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]Subscription, error)
	Save(ctx context.Context, sub Subscription) (Subscription, error)
	SaveMany(ctx context.Context, subs []Subscription) error
}
