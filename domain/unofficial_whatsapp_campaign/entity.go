package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
)

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

	ErrCampaignRunning = errors.New("unofficial whatsapp campaign cannot be edited while it is running")

	ErrCampaignResetCodeInvalid = errors.New("invalid reset confirmation code")
	ErrCampaignResetNotAllowed  = errors.New("unofficial whatsapp campaign reset not allowed while running")
	ErrCampaignClearCodeInvalid = errors.New("invalid clear history confirmation code")
	ErrCampaignClearNotAllowed  = errors.New("unofficial whatsapp campaign clear history not allowed while running")
)

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

type TargetInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Campaign struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspaceId"`
	DepartmentID string `json:"departmentId,omitempty"`
	InstanceID   string `json:"instanceId"`
	CreatedByID  string `json:"createdById,omitempty"`

	Name    string      `json:"name"`
	Message MessageSpec `json:"message"`

	InstanceLabel       string `json:"instanceLabel,omitempty"`
	InstanceStatus      string `json:"instanceStatus,omitempty"`
	InstanceSessionLive bool   `json:"instanceSessionLive"`

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

	SendDelayMinMS int `json:"sendDelayMinMs"`
	SendDelayMaxMS int `json:"sendDelayMaxMs"`
	DailyCap       int `json:"dailyCap"`

	Status       campaign.Status `json:"status"`
	StatusReason string          `json:"statusReason,omitempty"`

	ResetCode string `json:"resetCode,omitempty"`
	ClearCode string `json:"clearCode,omitempty"`

	ScheduledStart time.Time `json:"scheduledStart,omitempty"`
	Archived       bool      `json:"archived"`

	Targets       []TargetInput     `json:"targets,omitempty"`
	RecentEntries []Entry           `json:"recentEntries,omitempty"`
	Metrics       *campaign.Metrics `json:"metrics,omitempty"`

	SeedOutcome *campaign.SeededOutcome `json:"seedOutcome,omitempty"`

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
	c.SeedOutcome.Normalize()

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

const MinTargetDigits = 8

const MaxTargetDigits = 15

func ValidTargetNumber(raw string) bool {
	digits := uw.NormalizePhone(raw)
	return len(digits) >= MinTargetDigits && len(digits) <= MaxTargetDigits
}

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

func (c *Campaign) EffectiveDailyCap(instanceCap int) int {
	if c.DailyCap <= 0 {
		return instanceCap
	}
	if instanceCap > 0 && instanceCap < c.DailyCap {
		return instanceCap
	}
	return c.DailyCap
}

func (c *Campaign) Validate() error {
	if err := c.ValidateMetadata(); err != nil {
		return err
	}
	if err := c.validateSchedule(); err != nil {
		return err
	}
	if err := c.SeedOutcome.Validate(); err != nil {
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
