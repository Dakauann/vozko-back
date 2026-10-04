package webchat

import (
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxVisitorNameRunes  = 80
	MaxVisitorEmailBytes = 254
	MaxUserAgentRunes    = 256
	minPhoneDigits       = 10
	maxPhoneDigits       = 15
	localPhoneMaxDigits  = 11
	anonymousIDPrefixLen = 6
)

type Visitor struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspaceId"`
	WidgetID    string  `json:"widgetId"`
	ExternalID  *string `json:"externalId,omitempty"`

	Name   string  `json:"name,omitempty"`
	Email  string  `json:"email,omitempty"`
	Phone  string  `json:"phone,omitempty"`
	LeadID *string `json:"leadId,omitempty"`

	IdentityVerified  bool       `json:"identityVerified"`
	IntakeCompletedAt *time.Time `json:"intakeCompletedAt,omitempty"`
	ConsentedAt       *time.Time `json:"consentedAt,omitempty"`

	Locale     string     `json:"locale,omitempty"`
	UserAgent  string     `json:"-"`
	IPHash     string     `json:"-"`
	PageOrigin string     `json:"pageOrigin,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`

	Blocked   bool       `json:"blocked"`
	BlockedAt *time.Time `json:"blockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (v *Visitor) DisplayName() string {
	switch {
	case v.Name != "":
		return v.Name
	case v.Email != "":
		return v.Email
	case v.Phone != "":
		return "+" + v.Phone
	}
	short := v.ID
	if len(short) > anonymousIDPrefixLen {
		short = short[:anonymousIDPrefixLen]
	}
	return "Visitante #" + short
}

func (v *Visitor) Handle() string {
	if v.Email != "" {
		return v.Email
	}
	if v.Phone != "" {
		return "+" + v.Phone
	}
	return ""
}

func (v *Visitor) IntakePending(w *Widget) bool {
	return w.AsksIntake() && v.IntakeCompletedAt == nil
}

func (v *Visitor) MustCompleteIntake(w *Widget) bool {
	if !v.IntakePending(w) {
		return false
	}
	return w.IntakeName == FieldRequired || w.IntakeEmail == FieldRequired ||
		w.IntakePhone == FieldRequired || w.PrivacyPolicyURL != ""
}

func (v *Visitor) Verified() Intake {
	if !v.IdentityVerified {
		return Intake{}
	}
	return Intake{Name: v.Name, Email: v.Email, Phone: v.Phone}
}

type IntakeAnswers struct {
	Name    string
	Email   string
	Phone   string
	Consent bool
}

type Intake struct {
	Name        string
	Email       string
	Phone       string
	ConsentedAt *time.Time
}

func (w *Widget) ResolveIntake(answers IntakeAnswers, verified Intake, now time.Time) (Intake, error) {
	name, err := resolveField(w.IntakeName, verified.Name, answers.Name, cleanName, ErrIntakeNameInvalid)
	if err != nil {
		return Intake{}, err
	}
	email, err := resolveField(w.IntakeEmail, verified.Email, answers.Email, cleanEmail, ErrIntakeEmailInvalid)
	if err != nil {
		return Intake{}, err
	}
	phone, err := resolveField(w.IntakePhone, verified.Phone, answers.Phone, func(raw string) string {
		return NormalizePhone(raw, w.DefaultCountryCode)
	}, ErrIntakePhoneInvalid)
	if err != nil {
		return Intake{}, err
	}

	out := Intake{Name: name, Email: email, Phone: phone}
	if w.PrivacyPolicyURL != "" {
		if !answers.Consent {
			return Intake{}, ErrIntakeConsentRequired
		}
		stamped := now.UTC()
		out.ConsentedAt = &stamped
	}
	return out, nil
}

func resolveField(rule FieldRule, verified, typed string, clean func(string) string, invalid error) (string, error) {
	if !rule.Asked() {
		return "", nil
	}
	if verified != "" {
		return verified, nil
	}
	typed = strings.TrimSpace(typed)
	if typed == "" {
		if rule == FieldRequired {
			return "", ErrIntakeFieldRequired
		}
		return "", nil
	}
	value := clean(typed)
	if value == "" {
		return "", invalid
	}
	return value, nil
}

func cleanName(raw string) string {
	name := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(name) > MaxVisitorNameRunes {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return name
}

func cleanEmail(raw string) string {
	if len(raw) > MaxVisitorEmailBytes {
		return ""
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Name != "" || addr.Address != raw {
		return ""
	}
	return strings.ToLower(addr.Address)
}

func NormalizePhone(raw, defaultCountryCode string) string {
	raw = strings.TrimSpace(raw)
	international := strings.HasPrefix(raw, "+")
	if international {
		raw = raw[1:]
	}
	var digits strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return ""
		}
	}
	number := digits.String()
	if !international && len(number) >= minPhoneDigits && len(number) <= localPhoneMaxDigits {
		number = defaultCountryCode + number
	}
	if len(number) < minPhoneDigits || len(number) > maxPhoneDigits {
		return ""
	}
	return number
}

func TruncateUserAgent(raw string) string {
	raw = strings.TrimSpace(raw)
	if utf8.RuneCountInString(raw) <= MaxUserAgentRunes {
		return raw
	}
	return string([]rune(raw)[:MaxUserAgentRunes])
}
