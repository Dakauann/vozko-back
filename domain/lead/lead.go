package lead

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

var (
	ErrLeadRequired             = errors.New("lead: phone number is required")
	ErrLeadInvalid              = errors.New("lead: phone number is invalid")
	ErrLeadNotFound             = errors.New("lead: lead not found")
	ErrLeadDuplicate            = errors.New("lead: phone number already exists")
	ErrLeadWorkspaceRequired    = errors.New("lead: workspace id is required")
	ErrLeadFilterInvalid        = errors.New("lead: invalid filter")
	ErrLeadNameTooLong          = errors.New("lead: name is too long")
	ErrLeadIdentityRequired     = errors.New("lead: a name or a phone number is required")
	ErrLeadNicknameTooLong      = errors.New("lead: nickname is too long")
	ErrLeadEmailInvalid         = errors.New("lead: e-mail is invalid")
	ErrLeadBirthDateInvalid     = errors.New("lead: birth date is invalid")
	ErrLeadOwnerInvalid         = errors.New("lead: owner must be a member, an agent or a workflow")
	ErrLeadSourceInvalid        = errors.New("lead: the source of the incoming data is required")
	ErrLeadConsentSourceInvalid = errors.New("lead: the consent source is not a known one")
	ErrLeadOptOutSourceInvalid  = errors.New("lead: the opt-out source must be lead_request or operator")
	ErrLeadAgeInvalid           = errors.New("lead: age is invalid")
)

type OptOutSource string

const (
	OptOutLeadRequest OptOutSource = "lead_request"
	OptOutOperator    OptOutSource = "operator"
)

func (s OptOutSource) Valid() bool {
	return s == OptOutLeadRequest || s == OptOutOperator
}

type Source string

const (
	SourceManual  Source = "manual"
	SourceImport  Source = "import"
	SourceChannel Source = "channel"
)

func (s Source) Valid() bool {
	switch s {
	case SourceManual, SourceImport, SourceChannel:
		return true
	}
	return false
}

type ConsentSource string

const (
	ConsentForm                ConsentSource = "form"
	ConsentImport              ConsentSource = "import"
	ConsentManual              ConsentSource = "manual"
	ConsentConversationRequest ConsentSource = "conversation_request"
)

func (s ConsentSource) Valid() bool {
	switch s {
	case ConsentForm, ConsentImport, ConsentManual, ConsentConversationRequest:
		return true
	}
	return false
}

type Consent struct {
	GrantedAt time.Time     `json:"grantedAt"`
	Source    ConsentSource `json:"source"`
	Purpose   string        `json:"purpose,omitempty"`
}

type Lead struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspaceId"`
	Number            string         `json:"number"`
	Name              string         `json:"name,omitempty"`
	NameSource        Source         `json:"nameSource,omitempty"`
	Nickname          string         `json:"nickname,omitempty"`
	Email             string         `json:"email,omitempty"`
	BirthDate         *shared.Date   `json:"birthDate,omitempty"`
	Source            Source         `json:"source,omitempty"`
	Owner             string         `json:"owner,omitempty"`
	CustomFields      map[string]any `json:"customFields,omitempty"`
	WhatsAppOptIn     *Consent       `json:"whatsappOptIn,omitempty"`
	OptedOutAt        *time.Time     `json:"optedOutAt,omitempty"`
	OptOutSource      OptOutSource   `json:"optOutSource,omitempty"`
	Blocked           bool           `json:"blocked"`
	BlockedAt         time.Time      `json:"blockedAt"`
	BlockedBy         *string        `json:"blockedBy"`
	ProfilePictureURL string         `json:"profilePictureUrl,omitempty"`
	StoredAge         *int           `json:"age,omitempty"`
	Phones            []ContactPhone `json:"phones,omitempty"`
	Addresses         []Address      `json:"addresses,omitempty"`
	Relations         []Relation     `json:"relations,omitempty"`
	RelativesCount    int            `json:"relativesCount"`
	ReferredCount     int            `json:"referredCount"`
	Version           int64          `json:"version"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

func (l *Lead) Validate() error {
	if strings.TrimSpace(l.WorkspaceID) == "" {
		return ErrLeadWorkspaceRequired
	}
	if l.Number != "" && NormalizeNumber(l.Number) == "" {
		return ErrLeadInvalid
	}
	return l.ValidateRecord()
}

func (l *Lead) ValidateRecord() error {
	if strings.TrimSpace(l.WorkspaceID) == "" {
		return ErrLeadWorkspaceRequired
	}
	if !l.HasIdentity() && l.RealName() == "" {
		return ErrLeadIdentityRequired
	}
	if utf8.RuneCountInString(l.Nickname) > MaxLeadNameLength {
		return ErrLeadNicknameTooLong
	}
	if l.Email != "" {
		if err := ValidateEmail(l.Email); err != nil {
			return err
		}
	}
	if l.StoredAge != nil && *l.StoredAge < 0 {
		return ErrLeadAgeInvalid
	}
	if !validOwner(l.Owner) {
		return ErrLeadOwnerInvalid
	}
	if l.WhatsAppOptIn != nil && !l.WhatsAppOptIn.Source.Valid() {
		return ErrLeadConsentSourceInvalid
	}
	if err := l.validatePhones(); err != nil {
		return err
	}
	return l.validateAddresses()
}

func (l *Lead) HasIdentity() bool {
	return strings.TrimSpace(l.Number) != ""
}

func (l *Lead) Age(now time.Time) *int {
	if l.BirthDate != nil {
		years := l.BirthDate.YearsAt(now)
		return &years
	}
	return l.StoredAge
}

type LeadUpdate struct {
	Source            Source
	Name              string
	ProfilePictureURL string
}

func NormalizeNumber(value string) string {
	return shared.CanonicalPhoneNumber(value)
}

func NormalizeWhatsAppNumber(number string) string {
	normalized := NormalizeNumber(number)
	if normalized == "" {
		normalized = NormalizeRawNumber(number)
		if normalized == "" {
			return number
		}
	}
	if len(normalized) == 12 {
		if alternate := GetAlternatePhoneFormat(normalized); alternate != "" {
			return alternate
		}
	}
	return normalized
}

func GetAlternatePhoneFormat(number string) string {
	if variants := shared.NinthDigitVariants(number); len(variants) == 2 {
		return variants[1]
	}
	return ""
}

func NormalizeRawNumber(value string) string {
	return shared.BrazilPhoneDigits(value)
}

const MaxLeadNameLength = 120

func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) > MaxLeadNameLength {
		return ErrLeadNameTooLong
	}
	return nil
}

func NormalizeName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func NumberFormats(number string) []string {
	return shared.NinthDigitVariants(number)
}
