package lead

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrLeadRequired          = errors.New("lead: phone number is required")
	ErrLeadInvalid           = errors.New("lead: phone number is invalid")
	ErrLeadNotFound          = errors.New("lead: lead not found")
	ErrLeadDuplicate         = errors.New("lead: phone number already exists")
	ErrLeadWorkspaceRequired = errors.New("lead: workspace id is required")
	// ErrLeadFilterInvalid means the caller sent a filter expression the lead
	// object cannot answer (an unknown field, an operator the field does not
	// support, a malformed date). It is a 400, never a 500: the query is wrong,
	// not the database.
	ErrLeadFilterInvalid = errors.New("lead: invalid filter")
	// ErrLeadNameTooLong is a name a person typed that will not fit the surfaces
	// it has to render in.
	ErrLeadNameTooLong = errors.New("lead: name is too long")
)

type Lead struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspaceId"`
	Number            string    `json:"number"`
	Blocked           bool      `json:"blocked"`
	BlockedAt         time.Time `json:"blockedAt"`
	BlockedBy         *string   `json:"blockedBy"`
	Name              string    `json:"name,omitempty"`
	ProfilePictureURL string    `json:"profilePictureUrl,omitempty"`
	Age               *int      `json:"age,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (l *Lead) Normalize() {
	l.ID = strings.TrimSpace(l.ID)
	l.WorkspaceID = strings.TrimSpace(l.WorkspaceID)
	l.Number = NormalizeNumber(l.Number)
	l.Name = strings.TrimSpace(l.Name)
	if l.Age != nil && *l.Age <= 0 {
		l.Age = nil
	}
}

func (l *Lead) Validate() error {
	if l.WorkspaceID == "" {
		return ErrLeadWorkspaceRequired
	}
	if l.Number == "" {
		return ErrLeadRequired
	}
	if NormalizeNumber(l.Number) == "" {
		return ErrLeadInvalid
	}
	if l.Age != nil && *l.Age < 0 {
		return ErrLeadInvalid
	}
	return nil
}

func (l *Lead) Merge(update LeadUpdate) {
	if update.Name != "" {
		l.Name = update.Name
	}
	if update.ProfilePictureURL != "" {
		l.ProfilePictureURL = update.ProfilePictureURL
	}
	if update.Age != nil {
		l.Age = update.Age
	}
	if update.Blocked != nil {
		l.Blocked = *update.Blocked
		if *update.Blocked {
			if l.BlockedAt.IsZero() {
				l.BlockedAt = time.Now()
			}
			if update.BlockedBy != nil {
				l.BlockedBy = update.BlockedBy
			}
		} else {
			l.BlockedAt = time.Time{}
			l.BlockedBy = nil
		}
	}
}

type LeadUpdate struct {
	Name              string
	ProfilePictureURL string
	Age               *int
	Blocked           *bool
	BlockedBy         *string
}

func NormalizeNumber(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))

	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return ""
		}
		builder.WriteRune(r)
	}

	number := builder.String()
	if number == "" {
		return ""
	}

	if len(number) != 12 && len(number) != 13 {
		return ""
	}

	if !strings.HasPrefix(number, "55") {
		return ""
	}

	return number
}

func NormalizeWhatsAppNumber(number string) string {
	normalized := NormalizeNumber(number)
	if normalized == "" {
		normalized = normalizeRawInput(number)
		if normalized == "" {
			return number
		}
	}

	if len(normalized) == 13 {
		return normalized
	}

	if len(normalized) == 12 && strings.HasPrefix(normalized, "55") {

		if normalized[4] >= '6' {
			return normalized[:4] + "9" + normalized[4:]
		}
		return normalized
	}

	return normalized
}

func GetAlternatePhoneFormat(number string) string {
	normalized := NormalizeNumber(number)
	if normalized == "" {
		return ""
	}

	if len(normalized) == 13 && strings.HasPrefix(normalized, "55") {
		if normalized[4] == '9' {
			return normalized[:4] + normalized[5:]
		}
	}

	// TODO: validate in prod if this is actually the case, i have seen some landline numbers starting with a 9

	if len(normalized) == 12 && strings.HasPrefix(normalized, "55") && normalized[4] >= '6' {
		return normalized[:4] + "9" + normalized[4:]
	}

	return ""
}

// NormalizeRawNumber normalizes a raw phone number that may lack the country
// code (e.g. a SIP From header like "84994409624") into the canonical BR format
// (12/13 digits, "55"-prefixed), or "" if it cannot be normalized. Use this for
// inbound caller IDs where the carrier omits the country code.
func NormalizeRawNumber(value string) string {
	return normalizeRawInput(value)
}

func normalizeRawInput(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, r := range trimmed {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}

	number := builder.String()
	if number == "" {
		return ""
	}

	if !strings.HasPrefix(number, "55") {
		if len(number) >= 10 && len(number) <= 11 {
			number = "55" + number
		}
	}

	if len(number) != 12 && len(number) != 13 {
		return ""
	}

	return number
}

// MaxLeadNameLength bounds a human-entered lead name.
//
// Generous — a full legal name with titles fits easily — but bounded, because
// this string is rendered in the inbox row, the CRM header and the conversation
// list, none of which have room for a pasted paragraph.
const MaxLeadNameLength = 120

// ValidateName checks a name an operator typed.
//
// Empty is VALID and meaningful: it clears the name so the lead shows its phone
// number again, the way removing a contact's name in WhatsApp does. That is the
// one thing LeadUpdate.Name cannot express — Merge reads empty as "leave it
// alone", which is right for a webhook merging partial provider data and wrong
// for a person deliberately erasing a name. The two callers want opposite
// things from the same empty string, so renaming gets its own path rather than
// a flag on the shared one.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) > MaxLeadNameLength {
		return ErrLeadNameTooLong
	}
	return nil
}

// NormalizeName is what gets stored: trimmed, with internal whitespace runs
// collapsed so "Ana   Maria" and "Ana Maria" are not two different leads to the
// eye in a list.
func NormalizeName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}
