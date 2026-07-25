package lead

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrLeadRequired          = errors.New("lead: phone number is required")
	ErrLeadInvalid           = errors.New("lead: phone number is invalid")
	ErrLeadNotFound          = errors.New("lead: lead not found")
	ErrLeadDuplicate         = errors.New("lead: phone number already exists")
	ErrLeadWorkspaceRequired = errors.New("lead: workspace id is required")
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
