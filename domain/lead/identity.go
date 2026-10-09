package lead

import (
	"errors"
	"strings"

	"vozko/domain/shared"
)

var (
	ErrIdentityInUse     = errors.New("lead: the WhatsApp number already has conversations and cannot change")
	ErrIdentityUnchecked = errors.New("lead: a new WhatsApp number goes through ChangeIdentity, which checks its conversations")
)

func (l *Lead) WouldChangeIdentity(raw string) bool {
	number, err := parseIdentity(raw)
	return err != nil || !l.isIdentity(number)
}

func (l *Lead) isIdentity(number string) bool {
	return number == l.Number || (number != "" && sameNumber(number, l.Number))
}

func parseIdentity(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	number, err := shared.ParsePhone(raw)
	if err != nil {
		return "", ErrLeadInvalid
	}
	return number, nil
}

func (l *Lead) ChangeIdentity(raw string, inUse bool) (bool, error) {
	number, err := parseIdentity(raw)
	if err != nil {
		return false, err
	}
	if l.isIdentity(number) {
		return false, nil
	}
	if inUse {
		return false, ErrIdentityInUse
	}
	next := *l
	next.Number = number
	if number != "" {
		next.Phones, _ = l.withoutPhone(number)
	}
	if err := next.ValidateRecord(); err != nil {
		return false, err
	}
	*l = next
	return true, nil
}

func (l *Lead) PromoteContactPhone(number string) error {
	if l.HasIdentity() {
		return ErrIdentityInUse
	}
	canonical, err := shared.ParsePhone(number)
	if err != nil {
		return ErrLeadInvalid
	}
	kept, found := l.withoutPhone(canonical)
	if !found {
		return ErrPhoneUnknown
	}
	next := *l
	next.Number, next.Phones = canonical, kept
	if err := next.ValidateRecord(); err != nil {
		return err
	}
	*l = next
	return nil
}

func PromotionCandidate(number string, holders []*Lead) *Lead {
	var candidate *Lead
	for _, holder := range holders {
		if holder == nil || holder.HasIdentity() || !holder.HoldsNumber(number) {
			continue
		}
		if candidate != nil {
			return nil
		}
		candidate = holder
	}
	return candidate
}
