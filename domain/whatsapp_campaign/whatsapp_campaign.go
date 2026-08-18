package whatsapp_campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/lead"
	wce "vozko/domain/whatsapp_campaign_entry"
)

const MaxCampaignPhoneNumbers = 150000

var (
	ErrCampaignNameRequired              = errors.New("whatsapp campaign name is required")
	ErrCampaignTemplateIDRequired        = errors.New("whatsapp campaign template id is required")
	ErrCampaignBusinessPhoneIDRequired   = errors.New("whatsapp campaign business phone id is required")
	ErrCampaignScheduledStartInvalid     = errors.New("whatsapp campaign scheduled start time is invalid")
	ErrCampaignScheduledStartTooSoon     = errors.New("whatsapp campaign scheduled start time must be at least 5 minutes in the future and no more than 1 year from now")
	ErrCampaignBusinessPhoneNotFound     = errors.New("whatsapp campaign business phone not found")
	ErrCampaignBusinessPhoneNoAccess     = errors.New("user does not have access to this business phone number")
	ErrCampaignPhoneNumbersRequired      = errors.New("whatsapp campaign must contain at least one phone number")
	ErrCampaignPhoneNumbersTooMany       = errors.New("whatsapp campaign phone numbers exceed allowed limit")
	ErrCampaignPhoneNumberInvalid        = errors.New("whatsapp campaign phone number is invalid, must be E.164 format starting with 55 (e.g., 558499999999)")
	ErrCampaignNotFound                  = errors.New("whatsapp campaign not found")
	ErrCampaignStatusInvalid             = errors.New("whatsapp campaign status is invalid")
	ErrCampaignResetCodeInvalid          = errors.New("invalid reset confirmation code")
	ErrCampaignResetNotAllowed           = errors.New("whatsapp campaign reset not allowed while running")
	ErrCampaignTemplateNotFound          = errors.New("whatsapp campaign template not found or not approved")
	ErrCampaignTemplateNotApproved       = errors.New("whatsapp campaign template is not approved by Meta")
	ErrCampaignTemplateVariablesMismatch = errors.New("phone number variables count does not match template parameters - check your template's {{1}}, {{2}}, etc. placeholders and ensure each phone number has enough variables")
	ErrCampaignTemplateVariableEmpty     = errors.New("phone number template variable cannot be empty - each variable must contain a non-empty value")
	ErrCampaignTemplateNamedParameters   = errors.New("whatsapp campaign only supports templates with positional parameters {{1}}, {{2}}, etc. - templates with named parameters like {{name}} are not allowed")
	ErrCampaignRunning                   = errors.New("whatsapp campaign operation not allowed while running")
	ErrCampaignTemplatePhoneMismatch     = errors.New("template does not belong to the same WABA as the selected business phone - choose a template owned by the same WhatsApp Business Account")
	ErrEntryNotFound                     = errors.New("whatsapp campaign entry not found")
	ErrEntryDuplicate                    = errors.New("phone number already exists in this whatsapp campaign")
	ErrCampaignWorkflowVarsMissing       = errors.New("workflow requires campaign variables that are missing from phone number metadata")
)

type TemplateNotReadyError struct {
	TemplateName string
	Reason       string
}

func (e *TemplateNotReadyError) Error() string {
	return fmt.Sprintf("template '%s' is not ready for campaign use: %s", e.TemplateName, e.Reason)
}

func NewTemplateNotReadyError(templateName, reason string) *TemplateNotReadyError {
	return &TemplateNotReadyError{
		TemplateName: templateName,
		Reason:       reason,
	}
}

type CampaignType string

const (
	CampaignTypeStandard CampaignType = "standard"
	CampaignTypeOrganic  CampaignType = "organic"
)

func (t CampaignType) IsValid() bool {
	switch t {
	case CampaignTypeStandard, CampaignTypeOrganic:
		return true
	default:
		return false
	}
}

type Status string

const (
	CampaignStatusRunning   Status = "RUNNING"
	CampaignStatusPaused    Status = "PAUSED"
	CampaignStatusStopped   Status = "STOPPED"
	CampaignStatusCompleted Status = "COMPLETED"
)

func (s Status) IsValid() bool {
	switch s {
	case CampaignStatusRunning, CampaignStatusPaused, CampaignStatusStopped, CampaignStatusCompleted:
		return true
	default:
		return false
	}
}

func normalizeStatus(value Status) Status {
	trimmed := Status(strings.ToUpper(strings.TrimSpace(string(value))))
	if trimmed == "" {
		return CampaignStatusStopped
	}
	return trimmed
}

type CampaignMetrics struct {
	TotalNumbers            int64 `json:"totalNumbers"`
	Pending                 int64 `json:"pending"`
	Sent                    int64 `json:"sent"`
	Delivered               int64 `json:"delivered"`
	Read                    int64 `json:"read"`
	Failed                  int64 `json:"failed"`
	NotEligiblePossibleSpam int64 `json:"notEligiblePossibleSpam"`
	Processed               int64 `json:"processed"`
	// Dispatches ("disparos") is the real outbound send count: excludes
	// spam-protection skips and failed sends. See StatusCounts.Dispatches.
	// This is CURRENT entry status, not a ledger: a campaign reset zeroes it.
	Dispatches     int64   `json:"dispatches"`
	CompletionRate float64 `json:"completionRate"`
	SuccessRate    float64 `json:"successRate"`
	// ByCategory splits Dispatches by the campaign template's Meta category
	// (volume only, no money). Same current-state caveat as Dispatches.
	ByCategory *CategoryDispatches `json:"byCategory,omitempty"`
}

// CategoryDispatches is billed-send volume by WhatsApp template category.
// Populated on workspace summary; values are current entry statuses (reset clears).
type CategoryDispatches struct {
	Marketing      int64 `json:"marketing"`
	Utility        int64 `json:"utility"`
	Authentication int64 `json:"authentication"`
}

func NewCampaignMetrics(counts *wce.StatusCounts) *CampaignMetrics {
	if counts == nil {
		return &CampaignMetrics{}
	}

	metrics := &CampaignMetrics{
		TotalNumbers:            counts.Total,
		Pending:                 counts.Pending,
		Sent:                    counts.Sent,
		Delivered:               counts.Delivered,
		Read:                    counts.Read,
		Failed:                  counts.Failed,
		NotEligiblePossibleSpam: counts.NotEligiblePossibleSpam,
		Processed:               counts.Processed(),
		Dispatches:              counts.Dispatches(),
	}

	if metrics.TotalNumbers > 0 {
		metrics.CompletionRate = float64(metrics.Processed) / float64(metrics.TotalNumbers) * 100
	}
	if metrics.Processed > 0 {
		metrics.SuccessRate = float64(counts.Sent+counts.Delivered+counts.Read) / float64(metrics.Processed) * 100
	}

	return metrics
}

type PhoneInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Campaign struct {
	ID              string       `json:"id"`
	WorkspaceID     string       `json:"workspaceId"`
	DepartmentID    string       `json:"departmentId,omitempty"`
	BusinessPhoneID string       `json:"businessPhoneId,omitempty"`
	Name            string       `json:"name"`
	Type            CampaignType `json:"type"`
	TemplateID      string       `json:"templateId"`
	// TemplateName / TemplateCategory are list-enrichment fields (not stored
	// on the campaign row). Populated when the list use case can resolve the
	// linked WhatsApp template so the UI can show message type + charged $.
	TemplateName         string                      `json:"templateName,omitempty"`
	TemplateCategory     string                      `json:"templateCategory,omitempty"`
	AgentID              string                      `json:"agentId,omitempty"`
	WorkflowID           string                      `json:"workflowId,omitempty"`
	PipelineID           string                      `json:"pipelineId,omitempty"` // conversation funnel (empty = workspace default)
	EnableAgentResponses bool                        `json:"enableAgentResponses"`
	EnableWorkflow       bool                        `json:"enableWorkflow"`
	EnableAnalysis       bool                        `json:"enableAnalysis"`
	EnableAutoStaging    bool                        `json:"EnableAutoStaging"`
	EnableAutoMemory     bool                        `json:"enableAutoMemory"`
	PreferAudio          bool                        `json:"preferAudio"`
	ShowTemplateInCrm    bool                        `json:"showTemplateInCrm"`
	AiModel              string                      `json:"aiModel,omitempty"`
	Status               Status                      `json:"status"`
	ResetCode            string                      `json:"resetCode,omitempty"`
	ClearCode            string                      `json:"clearCode,omitempty"`
	PhoneInputs          []PhoneInput                `json:"phoneNumbers,omitempty"`
	RecentEntries        []wce.WhatsAppCampaignEntry `json:"recentEntries,omitempty"`
	Metrics              *CampaignMetrics            `json:"metrics,omitempty"`
	Archived             bool                        `json:"archived"`
	CreatedAt            time.Time                   `json:"createdAt"`
	UpdatedAt            time.Time                   `json:"updatedAt"`

	ScheduledStart time.Time `json:"scheduledStart,omitempty"`
}

func (c *Campaign) IsOrganic() bool {
	return c.Type == CampaignTypeOrganic
}

func (c *Campaign) Normalize() {
	c.ID = strings.TrimSpace(c.ID)
	c.DepartmentID = strings.TrimSpace(c.DepartmentID)
	c.Name = strings.TrimSpace(c.Name)
	c.TemplateID = strings.TrimSpace(c.TemplateID)
	c.BusinessPhoneID = strings.TrimSpace(c.BusinessPhoneID)
	c.AgentID = strings.TrimSpace(c.AgentID)
	c.WorkflowID = strings.TrimSpace(c.WorkflowID)
	c.PipelineID = strings.TrimSpace(c.PipelineID)
	c.Status = normalizeStatus(c.Status)
	if c.Type == "" {
		c.Type = CampaignTypeStandard
	}
	c.ScheduledStart = c.ScheduledStart.UTC()

	if len(c.PhoneInputs) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(c.PhoneInputs))
	clean := make([]PhoneInput, 0, len(c.PhoneInputs))
	for _, entry := range c.PhoneInputs {
		normalized := lead.NormalizeNumber(entry.Number)
		if normalized != "" {
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			entry.Number = normalized
		}
		clean = append(clean, entry)
	}
	c.PhoneInputs = clean
}

func (c *Campaign) Validate() error {
	if c.Name == "" {
		return ErrCampaignNameRequired
	}
	if c.IsOrganic() {
		if c.BusinessPhoneID == "" {
			return ErrCampaignBusinessPhoneIDRequired
		}
		if !c.Status.IsValid() {
			return ErrCampaignStatusInvalid
		}
		return nil
	}
	if c.TemplateID == "" {
		return ErrCampaignTemplateIDRequired
	}
	if c.BusinessPhoneID == "" {
		return ErrCampaignBusinessPhoneIDRequired
	}
	if !c.Status.IsValid() {
		return ErrCampaignStatusInvalid
	}
	if !c.ScheduledStart.IsZero() {
		if c.ScheduledStart.Before(time.Now()) {
			return ErrCampaignScheduledStartInvalid
		}
		if time.Until(c.ScheduledStart) < 5*time.Minute {
			return ErrCampaignScheduledStartTooSoon
		}
		if time.Until(c.ScheduledStart) > 365*24*time.Hour {
			return ErrCampaignScheduledStartTooSoon
		}
	}

	if len(c.PhoneInputs) == 0 {
		return ErrCampaignPhoneNumbersRequired
	}
	if len(c.PhoneInputs) > MaxCampaignPhoneNumbers {
		return ErrCampaignPhoneNumbersTooMany
	}
	for _, entry := range c.PhoneInputs {
		if lead.NormalizeNumber(entry.Number) == "" {
			return ErrCampaignPhoneNumberInvalid
		}
	}
	return nil
}

func (c *Campaign) ValidateMetadata() error {
	if c.Name == "" {
		return ErrCampaignNameRequired
	}
	if c.BusinessPhoneID == "" {
		return ErrCampaignBusinessPhoneIDRequired
	}
	if !c.Status.IsValid() {
		return ErrCampaignStatusInvalid
	}
	if c.IsOrganic() {
		return nil
	}
	if c.TemplateID == "" {
		return ErrCampaignTemplateIDRequired
	}
	return nil
}

func (c *Campaign) ValidateTemplateVariables(requiredParamCount int) error {
	if requiredParamCount == 0 {
		return nil
	}
	for _, entry := range c.PhoneInputs {
		if len(entry.Variables) < requiredParamCount {
			return ErrCampaignTemplateVariablesMismatch
		}
		for i := 0; i < requiredParamCount; i++ {
			if strings.TrimSpace(entry.Variables[i]) == "" {
				return ErrCampaignTemplateVariableEmpty
			}
		}
	}
	return nil
}

func (c *Campaign) ValidateWorkflowVars(requiredKeys []string) error {
	if len(requiredKeys) == 0 {
		return nil
	}
	for _, entry := range c.PhoneInputs {
		for _, key := range requiredKeys {
			val, exists := entry.Metadata[key]
			if !exists {
				return fmt.Errorf("%w: key %q missing for number %s",
					ErrCampaignWorkflowVarsMissing, key, entry.Number)
			}
			if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
				return fmt.Errorf("%w: key %q is empty for number %s",
					ErrCampaignWorkflowVarsMissing, key, entry.Number)
			}
		}
	}
	return nil
}
