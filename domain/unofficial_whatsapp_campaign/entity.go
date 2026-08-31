package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
)

// MaxCampaignTargets mirrors the Cloud API campaign's ceiling, so an operator
// who knows one product knows the other.
const MaxCampaignTargets = 150000

var (
	ErrCampaignNameRequired          = errors.New("unofficial whatsapp campaign name is required")
	ErrCampaignInstanceIDRequired    = errors.New("unofficial whatsapp campaign requires a connected number")
	ErrCampaignNotFound              = errors.New("unofficial whatsapp campaign not found")
	ErrCampaignStatusInvalid         = campaign.ErrStatusInvalid
	ErrCampaignTargetsRequired       = errors.New("unofficial whatsapp campaign must contain at least one number")
	ErrCampaignTargetsTooMany        = errors.New("unofficial whatsapp campaign numbers exceed the allowed limit")
	ErrCampaignTargetInvalid         = errors.New("unofficial whatsapp campaign number is not a usable international number")
	ErrCampaignVariablesMismatch     = errors.New("number variables count does not match the message placeholders - check the {{1}}, {{2}} markers and make sure every number carries enough values")
	ErrCampaignVariableEmpty         = errors.New("a campaign variable cannot be empty - every value must be filled in")
	ErrCampaignWorkflowVarsMissing   = errors.New("workflow requires campaign variables that are missing from a number's metadata")
	ErrCampaignScheduledStartTooSoon = errors.New("scheduled start must be at least 5 minutes in the future and no more than 1 year from now")
	ErrCampaignScheduledStartInvalid = errors.New("scheduled start is in the past")

	// ErrCampaignRunning guards edits to a live campaign.
	//
	// The official campaign does NOT guard this today, and that is a gap rather
	// than a precedent: changing a message mid-blast means one list receives two
	// different texts with no record of which recipient got which.
	ErrCampaignRunning = errors.New("unofficial whatsapp campaign cannot be edited while it is running")

	ErrCampaignResetCodeInvalid = errors.New("invalid reset confirmation code")
	ErrCampaignResetNotAllowed  = errors.New("unofficial whatsapp campaign reset not allowed while running")
	ErrCampaignClearCodeInvalid = errors.New("invalid clear history confirmation code")
	ErrCampaignClearNotAllowed  = errors.New("unofficial whatsapp campaign clear history not allowed while running")
)

// InstanceUnusableError explains why a number cannot run a campaign right now.
//
// A typed error rather than a sentinel because the remedy differs per cause and
// the UI has to say which: reconnect, wait until a restriction lifts, or nothing
// at all for a banned number.
type InstanceUnusableError struct {
	InstanceLabel string
	Reason        string
}

func (e *InstanceUnusableError) Error() string {
	return fmt.Sprintf("number %s cannot run a campaign: %s", e.InstanceLabel, e.Reason)
}

func NewInstanceUnusableError(label, reason string) *InstanceUnusableError {
	return &InstanceUnusableError{InstanceLabel: label, Reason: reason}
}

// TargetInput is one row of an imported list, shaped exactly like the official
// campaign's PhoneInput so the CSV importer and the create form transfer over
// unchanged.
type TargetInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Campaign is one bulk send over one connected number.
type Campaign struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspaceId"`
	DepartmentID string `json:"departmentId,omitempty"`
	InstanceID   string `json:"instanceId"`
	CreatedByID  string `json:"createdById,omitempty"`

	Name    string      `json:"name"`
	Message MessageSpec `json:"message"`

	// InstanceLabel / InstanceStatus / InstanceSessionLive are list-enrichment
	// fields resolved from the instance row, not stored on the campaign. The
	// same idiom as TemplateName on the official campaign: the screen needs to
	// show which number a campaign belongs to and whether it can send, without
	// a second request per row.
	InstanceLabel       string `json:"instanceLabel,omitempty"`
	InstanceStatus      string `json:"instanceStatus,omitempty"`
	InstanceSessionLive bool   `json:"instanceSessionLive"`

	// Automation: the same field set and the same meaning as the official
	// campaign, so one panel component drives both screens.
	AgentID              string `json:"agentId,omitempty"`
	WorkflowID           string `json:"workflowId,omitempty"`
	PipelineID           string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool   `json:"enableAgentResponses"`
	EnableWorkflow       bool   `json:"enableWorkflow"`
	EnableAnalysis       bool   `json:"enableAnalysis"`
	EnableAutoStaging    bool   `json:"enableAutoStaging"`
	EnableAutoMemory     bool   `json:"enableAutoMemory"`
	PreferAudio          bool   `json:"preferAudio"`
	AiModel              string `json:"aiModel,omitempty"`

	// Pacing is COPIED from the instance at creation, not referenced.
	//
	// Copying means a campaign can be made slower than its number without
	// slowing every other campaign on it, and — more importantly — that widening
	// the instance's range later cannot silently speed up a blast that is already
	// running against a number under scrutiny.
	SendDelayMinMS int `json:"sendDelayMinMs"`
	SendDelayMaxMS int `json:"sendDelayMaxMs"`
	// DailyCap is this campaign's own ceiling. The effective limit is always the
	// lower of this and the instance's warmup-adjusted cap; 0 means "no campaign
	// limit", never "unlimited".
	DailyCap int `json:"dailyCap"`

	Status campaign.Status `json:"status"`
	// StatusReason explains a pause the system applied rather than the operator.
	// Without it, a campaign that stopped itself for a WhatsApp restriction looks
	// identical to one somebody paused by hand.
	StatusReason string `json:"statusReason,omitempty"`

	ResetCode string `json:"resetCode,omitempty"`
	ClearCode string `json:"clearCode,omitempty"`

	ScheduledStart time.Time `json:"scheduledStart,omitempty"`
	Archived       bool      `json:"archived"`

	Targets       []TargetInput     `json:"targets,omitempty"`
	RecentEntries []Entry           `json:"recentEntries,omitempty"`
	Metrics       *campaign.Metrics `json:"metrics,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Campaign) Normalize() {
	c.ID = strings.TrimSpace(c.ID)
	c.WorkspaceID = strings.TrimSpace(c.WorkspaceID)
	c.DepartmentID = strings.TrimSpace(c.DepartmentID)
	c.InstanceID = strings.TrimSpace(c.InstanceID)
	c.Name = strings.TrimSpace(c.Name)
	c.AgentID = strings.TrimSpace(c.AgentID)
	c.WorkflowID = strings.TrimSpace(c.WorkflowID)
	c.PipelineID = strings.TrimSpace(c.PipelineID)
	c.Status = campaign.NormalizeStatus(c.Status)
	c.ScheduledStart = c.ScheduledStart.UTC()
	c.Message.Normalize()

	// Each bound falls back independently, for the reason the instance entity
	// spells out: defaulting only the minimum and clamping would collapse an
	// unset range to min..min, which is a FIXED cadence — the machine-regular
	// rhythm the jitter exists to avoid.
	if c.SendDelayMinMS <= 0 {
		c.SendDelayMinMS = uw.DefaultSendDelayMinMS
	}
	if c.SendDelayMaxMS <= 0 {
		c.SendDelayMaxMS = uw.DefaultSendDelayMaxMS
	}
	if c.SendDelayMinMS < uw.MinSendDelayMS {
		c.SendDelayMinMS = uw.MinSendDelayMS
	}
	if c.SendDelayMaxMS < c.SendDelayMinMS {
		c.SendDelayMaxMS = c.SendDelayMinMS
	}
	if c.DailyCap < 0 {
		c.DailyCap = 0
	}

	if len(c.Targets) == 0 {
		return
	}

	// Dedup by the NORMALIZED number, not the typed one: the same person written
	// "+55 84 99999-0001" and "5584999990001" is one person, and sending them a
	// campaign twice is the single most common cause of a ban complaint.
	seen := make(map[string]struct{}, len(c.Targets))
	clean := make([]TargetInput, 0, len(c.Targets))
	for _, t := range c.Targets {
		normalized := uw.NormalizePhone(t.Number)
		if normalized != "" {
			if _, dup := seen[normalized]; dup {
				continue
			}
			seen[normalized] = struct{}{}
			t.Number = normalized
		}
		t.Name = strings.TrimSpace(t.Name)
		clean = append(clean, t)
	}
	c.Targets = clean
}

// MinTargetDigits is the shortest string that could be a real international
// number. Below it the operator has mistyped rather than reached anyone, and
// sending to it is a wasted provider call against the number's send budget.
const MinTargetDigits = 8

// MaxTargetDigits is E.164's ceiling.
const MaxTargetDigits = 15

// ValidTargetNumber reports whether a number is usable as an address.
//
// Deliberately NOT Brazil-pinned, unlike the official campaign's
// ^55\d{10,11}$: this channel connects the customer's own handset and is used
// to reach numbers anywhere, so pinning a country here would refuse valid work.
func ValidTargetNumber(raw string) bool {
	digits := uw.NormalizePhone(raw)
	return len(digits) >= MinTargetDigits && len(digits) <= MaxTargetDigits
}

// SendDelayRange returns this campaign's clamped jitter bounds.
func (c *Campaign) SendDelayRange() (minMS, maxMS int) {
	minMS, maxMS = c.SendDelayMinMS, c.SendDelayMaxMS
	if minMS < uw.MinSendDelayMS {
		minMS = uw.MinSendDelayMS
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	return minMS, maxMS
}

// EffectiveDailyCap is the lower of this campaign's cap and the number's own.
//
// Always the lower, and a campaign cap of zero means "no campaign limit" rather
// than "no limit": the instance's ceiling still applies, because the ban risk
// belongs to the number and not to whoever configured this particular blast.
func (c *Campaign) EffectiveDailyCap(instanceCap int) int {
	if c.DailyCap <= 0 {
		return instanceCap
	}
	if instanceCap > 0 && instanceCap < c.DailyCap {
		return instanceCap
	}
	return c.DailyCap
}

// Validate is the full create-time check.
func (c *Campaign) Validate() error {
	if err := c.ValidateMetadata(); err != nil {
		return err
	}
	if err := c.validateSchedule(); err != nil {
		return err
	}

	if len(c.Targets) == 0 {
		return ErrCampaignTargetsRequired
	}
	if len(c.Targets) > MaxCampaignTargets {
		return ErrCampaignTargetsTooMany
	}
	for _, t := range c.Targets {
		if !ValidTargetNumber(t.Number) {
			return fmt.Errorf("%w: %q", ErrCampaignTargetInvalid, t.Number)
		}
	}
	return nil
}

// ValidateMetadata is the update-time check: everything except the target list,
// which an update never replaces wholesale.
func (c *Campaign) ValidateMetadata() error {
	if c.Name == "" {
		return ErrCampaignNameRequired
	}
	if c.InstanceID == "" {
		return ErrCampaignInstanceIDRequired
	}
	if !c.Status.IsValid() {
		return ErrCampaignStatusInvalid
	}
	return c.Message.Validate()
}

func (c *Campaign) validateSchedule() error {
	if c.ScheduledStart.IsZero() {
		return nil
	}
	if c.ScheduledStart.Before(time.Now()) {
		return ErrCampaignScheduledStartInvalid
	}
	if time.Until(c.ScheduledStart) < 5*time.Minute {
		return ErrCampaignScheduledStartTooSoon
	}
	if time.Until(c.ScheduledStart) > 365*24*time.Hour {
		return ErrCampaignScheduledStartTooSoon
	}
	return nil
}

// ValidateTargetVariables checks every target carries enough values for the
// message's placeholders.
func (c *Campaign) ValidateTargetVariables(required int) error {
	if required == 0 {
		return nil
	}
	for _, t := range c.Targets {
		if len(t.Variables) < required {
			return ErrCampaignVariablesMismatch
		}
		for i := 0; i < required; i++ {
			if strings.TrimSpace(t.Variables[i]) == "" {
				return ErrCampaignVariableEmpty
			}
		}
	}
	return nil
}

// ValidateWorkflowVars checks the metadata a linked workflow will read.
func (c *Campaign) ValidateWorkflowVars(requiredKeys []string) error {
	if len(requiredKeys) == 0 {
		return nil
	}
	for _, t := range c.Targets {
		for _, key := range requiredKeys {
			val, exists := t.Metadata[key]
			if !exists {
				return fmt.Errorf("%w: key %q missing for number %s", ErrCampaignWorkflowVarsMissing, key, t.Number)
			}
			if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
				return fmt.Errorf("%w: key %q is empty for number %s", ErrCampaignWorkflowVarsMissing, key, t.Number)
			}
		}
	}
	return nil
}
