package webchat

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/conversation"
)

const (
	MaxWidgetNameRunes     = 80
	MaxAllowedOrigins      = 20
	MaxShortTextRunes      = 60
	MaxWelcomeMessageRunes = 500
	MaxPrivacyURLBytes     = 2048
	DefaultAccentColor     = "#1F6FEB"
	DefaultCountryCode     = "55"
)

type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

func (s Status) Valid() bool { return s == StatusActive || s == StatusPaused }

type Position string

const (
	PositionRight Position = "right"
	PositionLeft  Position = "left"
)

func (p Position) Valid() bool { return p == PositionRight || p == PositionLeft }

type FieldRule string

const (
	FieldHidden   FieldRule = "hidden"
	FieldOptional FieldRule = "optional"
	FieldRequired FieldRule = "required"
)

func (r FieldRule) Valid() bool {
	return r == FieldHidden || r == FieldOptional || r == FieldRequired
}

func (r FieldRule) Asked() bool { return r == FieldOptional || r == FieldRequired }

type IdentityMode string

const (
	IdentityOff      IdentityMode = "off"
	IdentityOptional IdentityMode = "optional"
	IdentityRequired IdentityMode = "required"
)

func (m IdentityMode) Valid() bool {
	return m == IdentityOff || m == IdentityOptional || m == IdentityRequired
}

func (m IdentityMode) Verifies() bool { return m == IdentityOptional || m == IdentityRequired }

var hexColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
var countryCode = regexp.MustCompile(`^[0-9]{1,3}$`)

type Widget struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Name         string  `json:"name"`
	PublicKey    string  `json:"publicKey"`
	Status       Status  `json:"status"`

	AllowedOrigins []string `json:"allowedOrigins"`

	AccentColor    string   `json:"accentColor"`
	Position       Position `json:"position"`
	LauncherLabel  string   `json:"launcherLabel,omitempty"`
	WelcomeTitle   string   `json:"welcomeTitle,omitempty"`
	WelcomeMessage string   `json:"welcomeMessage,omitempty"`
	TeamName       string   `json:"teamName,omitempty"`
	AssistantName  string   `json:"assistantName,omitempty"`

	IntakeName         FieldRule `json:"intakeName"`
	IntakeEmail        FieldRule `json:"intakeEmail"`
	IntakePhone        FieldRule `json:"intakePhone"`
	PrivacyPolicyURL   string    `json:"privacyPolicyUrl,omitempty"`
	DefaultCountryCode string    `json:"defaultCountryCode"`

	AllowHumanRequest bool `json:"allowHumanRequest"`
	AllowAttachments  bool `json:"allowAttachments"`

	IdentityMode   IdentityMode `json:"identityMode"`
	IdentitySecret string       `json:"-"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (w *Widget) Normalize() {
	w.WorkspaceID = strings.TrimSpace(w.WorkspaceID)
	w.Name = strings.TrimSpace(w.Name)
	w.LauncherLabel = strings.TrimSpace(w.LauncherLabel)
	w.WelcomeTitle = strings.TrimSpace(w.WelcomeTitle)
	w.WelcomeMessage = strings.TrimSpace(w.WelcomeMessage)
	w.TeamName = strings.TrimSpace(w.TeamName)
	w.AssistantName = strings.TrimSpace(w.AssistantName)
	w.PrivacyPolicyURL = strings.TrimSpace(w.PrivacyPolicyURL)
	w.DefaultCountryCode = strings.TrimSpace(w.DefaultCountryCode)
	w.AccentColor = strings.ToUpper(strings.TrimSpace(w.AccentColor))

	if w.Status == "" {
		w.Status = StatusActive
	}
	if w.Position == "" {
		w.Position = PositionRight
	}
	if w.IdentityMode == "" {
		w.IdentityMode = IdentityOff
	}
	if w.IntakeName == "" {
		w.IntakeName = FieldOptional
	}
	if w.IntakeEmail == "" {
		w.IntakeEmail = FieldOptional
	}
	if w.IntakePhone == "" {
		w.IntakePhone = FieldHidden
	}
	if w.DefaultCountryCode == "" {
		w.DefaultCountryCode = DefaultCountryCode
	}
	if w.AccentColor == "" {
		w.AccentColor = DefaultAccentColor
	}
	w.AllowedOrigins = canonicalOrigins(w.AllowedOrigins)
}

func canonicalOrigins(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		key := strings.TrimSpace(r)
		if p, err := ParseOriginPattern(r); err == nil {
			key = p.String()
		}
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func (w *Widget) Validate() error {
	if w.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if strings.TrimSpace(w.Name) == "" {
		return ErrWidgetNameRequired
	}
	if utf8.RuneCountInString(w.Name) > MaxWidgetNameRunes {
		return ErrWidgetNameTooLong
	}
	if w.PublicKey == "" {
		return ErrWidgetPublicKeyMissing
	}
	if !w.Status.Valid() {
		return ErrWidgetStatusInvalid
	}
	if err := validateOrigins(w.AllowedOrigins); err != nil {
		return err
	}
	if !w.Position.Valid() {
		return ErrWidgetPositionInvalid
	}
	if !hexColour.MatchString(w.AccentColor) {
		return ErrWidgetColorInvalid
	}
	for _, short := range []string{w.LauncherLabel, w.WelcomeTitle, w.TeamName, w.AssistantName} {
		if utf8.RuneCountInString(short) > MaxShortTextRunes {
			return ErrWidgetTextTooLong
		}
	}
	if utf8.RuneCountInString(w.WelcomeMessage) > MaxWelcomeMessageRunes {
		return ErrWidgetTextTooLong
	}
	for _, rule := range []FieldRule{w.IntakeName, w.IntakeEmail, w.IntakePhone} {
		if !rule.Valid() {
			return ErrIntakeRuleInvalid
		}
	}
	if w.PrivacyPolicyURL != "" && !validPolicyURL(w.PrivacyPolicyURL) {
		return ErrPrivacyPolicyURLInvalid
	}
	if !countryCode.MatchString(w.DefaultCountryCode) {
		return ErrCountryCodeInvalid
	}
	if !w.IdentityMode.Valid() {
		return ErrIdentityModeInvalid
	}
	if w.IdentityMode.Verifies() && w.IdentitySecret == "" {
		return ErrIdentitySecretMissing
	}
	return nil
}

func validateOrigins(origins []string) error {
	if len(origins) == 0 {
		return ErrWidgetOriginsRequired
	}
	if len(origins) > MaxAllowedOrigins {
		return ErrWidgetTooManyOrigins
	}
	for _, o := range origins {
		if _, err := ParseOriginPattern(o); err != nil {
			return err
		}
	}
	return nil
}

func validPolicyURL(raw string) bool {
	if len(raw) > MaxPrivacyURLBytes {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func (w *Widget) originPatterns() []OriginPattern {
	out := make([]OriginPattern, 0, len(w.AllowedOrigins))
	for _, o := range w.AllowedOrigins {
		if p, err := ParseOriginPattern(o); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (w *Widget) AllowsOrigin(origin string) bool {
	for _, p := range w.originPatterns() {
		if p.Matches(origin) {
			return true
		}
	}
	return false
}

func (w *Widget) FrameAncestors() string {
	patterns := w.originPatterns()
	if len(patterns) == 0 {
		return "'none'"
	}
	parts := make([]string, len(patterns))
	for i, p := range patterns {
		parts[i] = p.String()
	}
	return strings.Join(parts, " ")
}

func (w *Widget) Serves() bool {
	return w != nil && w.Status == StatusActive && len(w.originPatterns()) > 0
}

func (w *Widget) AsksIntake() bool {
	return w.IntakeName.Asked() || w.IntakeEmail.Asked() || w.IntakePhone.Asked() || w.PrivacyPolicyURL != ""
}

func (w *Widget) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              w.AgentID,
		WorkflowID:           w.WorkflowID,
		EnableAgentResponses: w.EnableAgentResponses,
		EnableWorkflow:       w.EnableWorkflow,
		EnableAnalysis:       w.EnableAnalysis,
		EnableAutoStaging:    w.EnableAutoStaging,
		EnableAutoMemory:     w.EnableAutoMemory,
	}
}
