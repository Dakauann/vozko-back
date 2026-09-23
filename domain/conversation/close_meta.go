package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CloseSource string

const (
	CloseSourceHuman  CloseSource = "human"
	CloseSourceAI     CloseSource = "ai"
	CloseSourceSystem CloseSource = "system"
)

func (s CloseSource) Valid() bool {
	switch s {
	case CloseSourceHuman, CloseSourceAI, CloseSourceSystem:
		return true
	}
	return false
}

type CloseReason string

const (
	CloseReasonManual       CloseReason = "manual"
	CloseReasonCustomerIdle CloseReason = "customer_idle"
	CloseReasonAIResolved   CloseReason = "ai_resolved"
	CloseReasonMaxAge       CloseReason = "max_age"
	CloseReasonWorkflow     CloseReason = "workflow"
)

func (r CloseReason) Valid() bool {
	switch r {
	case CloseReasonManual, CloseReasonCustomerIdle, CloseReasonAIResolved, CloseReasonMaxAge, CloseReasonWorkflow:
		return true
	}
	return false
}

const ReservedOutcomePrefix = "_"

const (
	OutcomeSystemAutoClose     = "_system_auto_close"
	OutcomeAIUnspecified       = "_ai_unspecified"
	OutcomeWorkflowUnspecified = "_workflow_unspecified"
)

const (
	MaxOutcomeCodeLength = 64
	MaxOutcomeCatalogue  = 40
)

var (
	ErrOutcomeRequired       = errors.New("conversation: finishing this conversation requires an outcome")
	ErrOutcomeUnknown        = errors.New("conversation: outcome is not in the workspace catalogue")
	ErrOutcomeReserved       = errors.New("conversation: outcome codes cannot start with an underscore")
	ErrOutcomeCodeRequired   = errors.New("conversation: outcome code is required")
	ErrOutcomeCodeTooLong    = errors.New("conversation: outcome code is too long")
	ErrOutcomeLabelRequired  = errors.New("conversation: outcome label is required")
	ErrOutcomeDuplicate      = errors.New("conversation: duplicate outcome code")
	ErrOutcomeCatalogueEmpty = errors.New("conversation: outcome capture requires at least one outcome")
	ErrOutcomeCatalogueLarge = errors.New("conversation: outcome catalogue is too large")
	ErrOutcomeNoDurable      = errors.New("conversation: outcome capture requires at least one durable outcome")
	ErrOutcomeThreshold      = errors.New("conversation: durable threshold must be between 0 and 100")
)

type Outcome struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	IsDurable bool   `json:"isDurable"`
	Position  int    `json:"position"`
}

type OutcomeCapture struct {
	Enabled          bool       `json:"enabled"`
	EnabledAt        *time.Time `json:"enabledAt,omitempty"`
	RequireOnFinish  bool       `json:"requireOnFinish"`
	DurableThreshold float64    `json:"durableThreshold"`
	Outcomes         []Outcome  `json:"outcomes"`
	DepartmentIDs    []string   `json:"departmentIds,omitempty"`
}

const DefaultDurableThreshold = 30.0

func IsReservedOutcome(code string) bool {
	return strings.HasPrefix(code, ReservedOutcomePrefix)
}

func (c *OutcomeCapture) Normalize() {
	if c == nil {
		return
	}
	normalized := make([]Outcome, 0, len(c.Outcomes))
	for _, o := range c.Outcomes {
		o.Code = strings.ToLower(strings.TrimSpace(o.Code))
		o.Label = strings.TrimSpace(o.Label)
		if o.Code == "" && o.Label == "" {
			continue
		}
		normalized = append(normalized, o)
	}
	sort.SliceStable(normalized, func(i, j int) bool { return normalized[i].Position < normalized[j].Position })
	for i := range normalized {
		normalized[i].Position = i + 1
	}
	c.Outcomes = normalized

	departments := make([]string, 0, len(c.DepartmentIDs))
	seen := make(map[string]struct{}, len(c.DepartmentIDs))
	for _, id := range c.DepartmentIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		departments = append(departments, trimmed)
	}
	sort.Strings(departments)
	c.DepartmentIDs = departments

	if c.DurableThreshold == 0 {
		c.DurableThreshold = DefaultDurableThreshold
	}
}

func (c *OutcomeCapture) Validate() error {
	if c == nil {
		return nil
	}
	if c.DurableThreshold < 0 || c.DurableThreshold > 100 {
		return ErrOutcomeThreshold
	}
	if len(c.Outcomes) > MaxOutcomeCatalogue {
		return ErrOutcomeCatalogueLarge
	}

	seen := make(map[string]struct{}, len(c.Outcomes))
	durable := false
	for _, o := range c.Outcomes {
		if o.Code == "" {
			return ErrOutcomeCodeRequired
		}
		if len(o.Code) > MaxOutcomeCodeLength {
			return ErrOutcomeCodeTooLong
		}
		if IsReservedOutcome(o.Code) {
			return ErrOutcomeReserved
		}
		if o.Label == "" {
			return ErrOutcomeLabelRequired
		}
		if _, exists := seen[o.Code]; exists {
			return ErrOutcomeDuplicate
		}
		seen[o.Code] = struct{}{}
		if o.IsDurable {
			durable = true
		}
	}
	if !c.Enabled {
		return nil
	}
	if len(c.Outcomes) == 0 {
		return ErrOutcomeCatalogueEmpty
	}
	if !durable {
		return ErrOutcomeNoDurable
	}
	return nil
}

func (c *OutcomeCapture) AppliesTo(departmentID string, at time.Time) bool {
	if c == nil || !c.Enabled {
		return false
	}
	if c.EnabledAt != nil && at.Before(*c.EnabledAt) {
		return false
	}
	if len(c.DepartmentIDs) == 0 {
		return true
	}
	for _, id := range c.DepartmentIDs {
		if id == departmentID {
			return true
		}
	}
	return false
}

func (c *OutcomeCapture) Lookup(code string) (Outcome, bool) {
	if c == nil {
		return Outcome{}, false
	}
	normalized := strings.ToLower(strings.TrimSpace(code))
	for _, o := range c.Outcomes {
		if o.Code == normalized {
			return o, true
		}
	}
	return Outcome{}, false
}

func (c *OutcomeCapture) DurableCodes() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Outcomes))
	for _, o := range c.Outcomes {
		if o.IsDurable {
			out = append(out, o.Code)
		}
	}
	return out
}

func (c *OutcomeCapture) Resolve(source CloseSource, reason CloseReason, code string, departmentID string, at time.Time) (string, error) {
	if !c.AppliesTo(departmentID, at) {
		return "", nil
	}

	switch source {
	case CloseSourceSystem:
		if reason == CloseReasonWorkflow {
			if strings.TrimSpace(code) == "" {
				return OutcomeWorkflowUnspecified, nil
			}
			return c.validated(code)
		}
		return OutcomeSystemAutoClose, nil
	case CloseSourceAI:
		if strings.TrimSpace(code) == "" {
			return OutcomeAIUnspecified, nil
		}
		return c.validated(code)
	default:
		if strings.TrimSpace(code) == "" {
			if c.RequireOnFinish {
				return "", ErrOutcomeRequired
			}
			return "", nil
		}
		return c.validated(code)
	}
}

func (c *OutcomeCapture) validated(code string) (string, error) {
	outcome, found := c.Lookup(code)
	if !found {
		return "", ErrOutcomeUnknown
	}
	return outcome.Code, nil
}

type StatusWrite struct {
	Status         ConversationStatus
	SetCloseMeta   bool
	CloseSource    CloseSource
	CloseReason    CloseReason
	CloseOutcome   string
	ClosedAt       time.Time
	ClearCloseMeta bool
}

type AutoCloseCandidate struct {
	EntryID            string
	EntryType          string
	WorkspaceID        string
	LastAgentMessageAt time.Time
}

const DefaultAutoCloseIdleAfterHours = 24

const (
	MinAutoCloseIdleAfterHours = 1
	MaxAutoCloseIdleAfterHours = 168
)

func ClampAutoCloseIdleHours(hours int) int {
	if hours < MinAutoCloseIdleAfterHours {
		return DefaultAutoCloseIdleAfterHours
	}
	if hours > MaxAutoCloseIdleAfterHours {
		return MaxAutoCloseIdleAfterHours
	}
	return hours
}

func DecodeOutcomeCapturePatch(body []byte, field string) (capture *OutcomeCapture, clear bool, err error) {
	if len(body) == 0 {
		return nil, false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, nil
	}
	raw, present := envelope[field]
	if !present {
		return nil, false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, true, nil
	}
	var parsed OutcomeCapture
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, fmt.Errorf("conversation: %q must be an object or null: %w", field, err)
	}
	return &parsed, false, nil
}

var outcomePolicyErrors = []error{
	ErrOutcomeReserved,
	ErrOutcomeCodeRequired,
	ErrOutcomeCodeTooLong,
	ErrOutcomeLabelRequired,
	ErrOutcomeDuplicate,
	ErrOutcomeCatalogueEmpty,
	ErrOutcomeCatalogueLarge,
	ErrOutcomeNoDurable,
	ErrOutcomeThreshold,
}

func IsOutcomePolicyError(err error) bool {
	for _, sentinel := range outcomePolicyErrors {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
