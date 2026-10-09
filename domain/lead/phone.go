package lead

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

const MaxContactPhones = 6

var (
	ErrPhoneInvalid         = errors.New("lead: a contact phone is not a Brazilian number with area code")
	ErrPhoneLabelInvalid    = errors.New("lead: a contact phone label is not a known one")
	ErrPhoneLimit           = fmt.Errorf("lead: a lead keeps at most %d contact phones", MaxContactPhones)
	ErrPhoneRepeatsIdentity = errors.New("lead: a contact phone repeats the WhatsApp number")
	ErrPhoneRepeated        = errors.New("lead: a contact phone is listed twice")
	ErrPhoneUnknown         = errors.New("lead: a contact phone is not one of this lead's")
)

type PhoneLabel string

const (
	PhoneMobile   PhoneLabel = "mobile"
	PhoneLandline PhoneLabel = "landline"
	PhoneWork     PhoneLabel = "work"
	PhoneMessage  PhoneLabel = "message"
	PhoneOther    PhoneLabel = "other"
)

func (l PhoneLabel) Valid() bool {
	switch l {
	case PhoneMobile, PhoneLandline, PhoneWork, PhoneMessage, PhoneOther:
		return true
	}
	return false
}

type ContactPhone struct {
	ID        string     `json:"id,omitempty"`
	Number    string     `json:"number"`
	Label     PhoneLabel `json:"label"`
	CreatedAt time.Time  `json:"createdAt"`
}

type ItemError struct {
	Field string
	Index int
	Err   error
}

func (e *ItemError) Error() string {
	return fmt.Sprintf("%s[%d]: %v", e.Field, e.Index, e.Err)
}

func (e *ItemError) Unwrap() error {
	return e.Err
}

func itemError(field string, index int, err error) error {
	return &ItemError{Field: field, Index: index, Err: err}
}

func (l *Lead) SetPhones(inputs []ContactPhone) error {
	if len(inputs) > MaxContactPhones {
		return ErrPhoneLimit
	}
	phones := make([]ContactPhone, 0, len(inputs))
	for i, in := range inputs {
		number, err := shared.ParsePhone(in.Number)
		if err != nil {
			return itemError(FieldPhones, i, ErrPhoneInvalid)
		}
		stored, err := l.storedPhone(strings.TrimSpace(in.ID), number)
		if err != nil {
			return itemError(FieldPhones, i, err)
		}
		phones = append(phones, ContactPhone{ID: stored.ID, Number: number, Label: in.Label, CreatedAt: stored.CreatedAt})
	}
	next := *l
	next.Phones = phones
	if err := next.validatePhones(); err != nil {
		return err
	}
	l.Phones = phones
	return nil
}

func (l *Lead) storedPhone(id, number string) (ContactPhone, error) {
	for _, p := range l.Phones {
		if id != "" && p.ID == id {
			return p, nil
		}
		if id == "" && sameNumber(p.Number, number) {
			return p, nil
		}
	}
	if id != "" {
		return ContactPhone{}, ErrPhoneUnknown
	}
	return ContactPhone{}, nil
}

func (l *Lead) validatePhones() error {
	if len(l.Phones) > MaxContactPhones {
		return ErrPhoneLimit
	}
	identity := formatSet(l.Number)
	seen := map[string]bool{}
	for i, p := range l.Phones {
		if !p.Label.Valid() {
			return itemError(FieldPhones, i, ErrPhoneLabelInvalid)
		}
		formats := NumberFormats(p.Number)
		if len(formats) == 0 {
			return itemError(FieldPhones, i, ErrPhoneInvalid)
		}
		for _, format := range formats {
			if identity[format] {
				return itemError(FieldPhones, i, ErrPhoneRepeatsIdentity)
			}
			if seen[format] {
				return itemError(FieldPhones, i, ErrPhoneRepeated)
			}
		}
		for _, format := range formats {
			seen[format] = true
		}
	}
	return nil
}

func (l *Lead) Numbers() []string {
	numbers := make([]string, 0, len(l.Phones)+1)
	if l.HasIdentity() {
		numbers = append(numbers, l.Number)
	}
	for _, p := range l.Phones {
		numbers = append(numbers, p.Number)
	}
	return numbers
}

func (l *Lead) HoldsNumber(number string) bool {
	if strings.TrimSpace(number) == "" {
		return false
	}
	for _, own := range l.Numbers() {
		if sameNumber(own, number) {
			return true
		}
	}
	return false
}

func (l *Lead) HoldsIdentity(number string) bool {
	return l.HasIdentity() && strings.TrimSpace(number) != "" && sameNumber(l.Number, number)
}

func SameNumber(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(b) != "" && sameNumber(a, b)
}

func sameNumber(a, b string) bool {
	formats := formatSet(a)
	for _, format := range NumberFormats(b) {
		if formats[format] {
			return true
		}
	}
	return false
}

func formatSet(number string) map[string]bool {
	set := map[string]bool{}
	for _, format := range NumberFormats(number) {
		set[format] = true
	}
	return set
}

func (l *Lead) withoutPhone(number string) ([]ContactPhone, bool) {
	kept := make([]ContactPhone, 0, len(l.Phones))
	found := false
	for _, p := range l.Phones {
		if sameNumber(p.Number, number) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	return kept, found
}
