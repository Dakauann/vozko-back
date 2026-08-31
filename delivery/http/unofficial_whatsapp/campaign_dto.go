package unofficial_whatsapp

import (
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// The campaign wire shapes.
//
// Deliberately parallel to the official campaign's payload so one frontend
// component renders both, with three differences that ARE the product
// difference: a message spec where the official carries a templateId, pacing and
// cap fields the Cloud API has no use for, and nothing anywhere about money.

type messageSpecDTO struct {
	Kind   string   `json:"kind"`
	Bodies []string `json:"bodies"`

	MediaID  string `json:"mediaId,omitempty"`
	FileName string `json:"fileName,omitempty"`

	Style   string          `json:"style,omitempty"`
	Footer  string          `json:"footer,omitempty"`
	Button  string          `json:"button,omitempty"`
	Options []menuOptionDTO `json:"options,omitempty"`
}

type menuOptionDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

func (d messageSpecDTO) toDomain() uwc.MessageSpec {
	options := make([]uw.InteractiveOption, 0, len(d.Options))
	for _, o := range d.Options {
		options = append(options, uw.InteractiveOption{ID: o.ID, Title: o.Title, Description: o.Description})
	}
	return uwc.MessageSpec{
		Kind:     uwc.MessageKind(d.Kind),
		Bodies:   d.Bodies,
		MediaID:  d.MediaID,
		FileName: d.FileName,
		Style:    d.Style,
		Footer:   d.Footer,
		Button:   d.Button,
		Options:  options,
	}
}

func messageSpecToDTO(m uwc.MessageSpec) messageSpecDTO {
	options := make([]menuOptionDTO, 0, len(m.Options))
	for _, o := range m.Options {
		options = append(options, menuOptionDTO{ID: o.ID, Title: o.Title, Description: o.Description})
	}
	return messageSpecDTO{
		Kind:     string(m.Kind),
		Bodies:   m.Bodies,
		MediaID:  m.MediaID,
		FileName: m.FileName,
		Style:    m.Style,
		Footer:   m.Footer,
		Button:   m.Button,
		Options:  options,
	}
}

// campaignTargetDTO is one imported row, shaped exactly like the official
// campaign's phone-number payload so the CSV importer transfers unchanged.
type campaignTargetDTO struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type campaignPayload struct {
	Name       string         `json:"name"`
	InstanceID string         `json:"instanceId"`
	Message    messageSpecDTO `json:"message"`

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

	SendDelayMinMS int `json:"sendDelayMinMs,omitempty"`
	SendDelayMaxMS int `json:"sendDelayMaxMs,omitempty"`
	DailyCap       int `json:"dailyCap,omitempty"`

	ScheduledStart *time.Time          `json:"scheduledStart,omitempty"`
	Archived       bool                `json:"archived"`
	Targets        []campaignTargetDTO `json:"targets,omitempty"`
}

func (p campaignPayload) toDomain(workspaceID string) *uwc.Campaign {
	targets := make([]uwc.TargetInput, 0, len(p.Targets))
	for _, t := range p.Targets {
		targets = append(targets, uwc.TargetInput{
			Number: t.Number, Name: t.Name, Variables: t.Variables, Metadata: t.Metadata,
		})
	}

	c := &uwc.Campaign{
		WorkspaceID:          workspaceID,
		InstanceID:           p.InstanceID,
		Name:                 p.Name,
		Message:              p.Message.toDomain(),
		AgentID:              p.AgentID,
		WorkflowID:           p.WorkflowID,
		PipelineID:           p.PipelineID,
		EnableAgentResponses: p.EnableAgentResponses,
		EnableWorkflow:       p.EnableWorkflow,
		EnableAnalysis:       p.EnableAnalysis,
		EnableAutoStaging:    p.EnableAutoStaging,
		EnableAutoMemory:     p.EnableAutoMemory,
		PreferAudio:          p.PreferAudio,
		AiModel:              p.AiModel,
		SendDelayMinMS:       p.SendDelayMinMS,
		SendDelayMaxMS:       p.SendDelayMaxMS,
		DailyCap:             p.DailyCap,
		Archived:             p.Archived,
		Targets:              targets,
	}
	if p.ScheduledStart != nil {
		c.ScheduledStart = *p.ScheduledStart
	}
	return c
}

// campaignDTO is what every campaign endpoint returns.
type campaignDTO struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	InstanceID   string  `json:"instanceId"`

	Name    string         `json:"name"`
	Message messageSpecDTO `json:"message"`

	// The number's identity and live state, resolved server-side so the screen
	// can disable Start without a second request — and so the UI cannot disagree
	// with the send path about whether this number may send.
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

	Status string `json:"status"`
	// StatusReason explains a pause the SYSTEM applied. Surfaced because an
	// automatic pause and a manual one look identical without it, and an
	// operator who cannot tell them apart restarts straight into the restriction.
	StatusReason string `json:"statusReason,omitempty"`

	ScheduledStart *time.Time `json:"scheduledStart,omitempty"`
	Archived       bool       `json:"archived"`

	Metrics   *campaign.Metrics `json:"metrics,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

func campaignToDTO(c *uwc.Campaign) campaignDTO {
	if c == nil {
		return campaignDTO{}
	}
	dto := campaignDTO{
		ID:                   c.ID,
		WorkspaceID:          c.WorkspaceID,
		InstanceID:           c.InstanceID,
		Name:                 c.Name,
		Message:              messageSpecToDTO(c.Message),
		InstanceLabel:        c.InstanceLabel,
		InstanceStatus:       c.InstanceStatus,
		InstanceSessionLive:  c.InstanceSessionLive,
		AgentID:              c.AgentID,
		WorkflowID:           c.WorkflowID,
		PipelineID:           c.PipelineID,
		EnableAgentResponses: c.EnableAgentResponses,
		EnableWorkflow:       c.EnableWorkflow,
		EnableAnalysis:       c.EnableAnalysis,
		EnableAutoStaging:    c.EnableAutoStaging,
		EnableAutoMemory:     c.EnableAutoMemory,
		PreferAudio:          c.PreferAudio,
		AiModel:              c.AiModel,
		SendDelayMinMS:       c.SendDelayMinMS,
		SendDelayMaxMS:       c.SendDelayMaxMS,
		DailyCap:             c.DailyCap,
		Status:               string(c.Status),
		StatusReason:         c.StatusReason,
		Archived:             c.Archived,
		Metrics:              c.Metrics,
		CreatedAt:            c.CreatedAt,
		UpdatedAt:            c.UpdatedAt,
	}
	if c.DepartmentID != "" {
		id := c.DepartmentID
		dto.DepartmentID = &id
	}
	if !c.ScheduledStart.IsZero() {
		start := c.ScheduledStart
		dto.ScheduledStart = &start
	}
	return dto
}

// campaignEntryDTO is one row of the entries table.
type campaignEntryDTO struct {
	ID         string `json:"id"`
	CampaignID string `json:"campaignId"`
	LeadID     string `json:"leadId"`
	Number     string `json:"number"`
	Name       string `json:"name,omitempty"`

	// ConversationID is what makes the row clickable through to a transcript.
	// Empty until the campaign has actually reached this person.
	ConversationID string `json:"conversationId,omitempty"`

	Status string `json:"status"`
	// VariantIndex answers "which message did this person get" for a campaign
	// running rotations.
	VariantIndex int `json:"variantIndex"`

	ErrorCode    int    `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`
	LastMessageAt      *time.Time `json:"lastMessageAt,omitempty"`
	SentAt             *time.Time `json:"sentAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func entryToDTO(e *uwc.EntryWithLead) campaignEntryDTO {
	if e == nil || e.Entry == nil {
		return campaignEntryDTO{}
	}
	return campaignEntryDTO{
		ID:                 e.Entry.ID,
		CampaignID:         e.Entry.CampaignID,
		LeadID:             e.Entry.LeadID,
		Number:             e.Entry.Number,
		Name:               e.Entry.Name,
		ConversationID:     e.Entry.ConversationID,
		Status:             string(e.Entry.Status),
		VariantIndex:       e.Entry.VariantIndex,
		ErrorCode:          e.Entry.ErrorCode,
		ErrorMessage:       e.Entry.ErrorMessage,
		Variables:          e.Entry.Variables,
		Metadata:           e.Entry.Metadata,
		ConversationStatus: e.ConversationStatus,
		AutomationEnabled:  e.AutomationEnabled,
		LastMessageAt:      e.LastMessageAt,
		SentAt:             e.Entry.SentAt,
		CreatedAt:          e.Entry.CreatedAt,
		UpdatedAt:          e.Entry.UpdatedAt,
	}
}
