package emailsending

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/emailaddress"
)

const maxDisplayNameLength = 80

var reservedLocalParts = map[string]struct{}{
	"bounce":        {},
	"reply":         {},
	"unsubscribe":   {},
	"postmaster":    {},
	"abuse":         {},
	"mailer-daemon": {},
}

type Sender struct {
	ID             string
	WorkspaceID    string
	DomainID       string
	LocalPart      string
	DisplayName    string
	ReplyForwardTo emailaddress.Address
	IsDefault      bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Mailbox struct {
	Name    string
	Address emailaddress.Address
}

func NewSender(domain SendingDomain, localPart, displayName, forwardTo string) (Sender, error) {
	if domain.Lifecycle() == LifecycleReleasing {
		return Sender{}, ErrDomainReleasing
	}
	if !domain.Sendable() {
		return Sender{}, ErrDomainNotVerified
	}
	if emailaddress.IsFreeMailbox(domain.Domain) {
		return Sender{}, ErrFreeMailboxFrom
	}
	local, err := normalizeLocalPart(localPart, domain.Domain)
	if err != nil {
		return Sender{}, err
	}
	name, err := normalizeDisplayName(displayName)
	if err != nil {
		return Sender{}, err
	}
	var forward emailaddress.Address
	if strings.TrimSpace(forwardTo) != "" {
		forward, err = emailaddress.Normalize(forwardTo)
		if err != nil {
			return Sender{}, ErrInvalidForward
		}
	}
	return Sender{
		WorkspaceID:    domain.WorkspaceID,
		DomainID:       domain.ID,
		LocalPart:      local,
		DisplayName:    name,
		ReplyForwardTo: forward,
	}, nil
}

func (s Sender) FromAddress(domain SendingDomain) emailaddress.Address {
	return emailaddress.Address(s.LocalPart + "@" + domain.Domain)
}

func (s Sender) Mailbox(domain SendingDomain) Mailbox {
	return Mailbox{Name: s.DisplayName, Address: s.FromAddress(domain)}
}

func (s Sender) CanSendFrom(domain SendingDomain) error {
	if s.DomainID != domain.ID {
		return ErrSenderDomainMismatch
	}
	if !domain.Sendable() {
		return ErrDomainNotVerified
	}
	if emailaddress.IsFreeMailbox(domain.Domain) {
		return ErrFreeMailboxFrom
	}
	return nil
}

func normalizeLocalPart(raw, domain string) (string, error) {
	local := strings.ToLower(strings.TrimSpace(raw))
	if local == "" {
		return "", ErrInvalidLocalPart
	}
	if _, reserved := reservedLocalParts[local]; reserved {
		return "", ErrInvalidLocalPart
	}
	if _, err := emailaddress.Normalize(local + "@" + domain); err != nil {
		return "", ErrInvalidLocalPart
	}
	return local, nil
}

func normalizeDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxDisplayNameLength {
		return "", ErrInvalidDisplayName
	}
	if strings.ContainsAny(name, "\r\n\x00\"<>") {
		return "", ErrInvalidDisplayName
	}
	return name, nil
}
