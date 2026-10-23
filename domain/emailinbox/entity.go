package emailinbox

import (
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

type Inbox struct {
	ID           string               `json:"id"`
	WorkspaceID  string               `json:"workspaceId"`
	Name         string               `json:"name"`
	Address      emailaddress.Address `json:"address"`
	DepartmentID *string              `json:"departmentId,omitempty"`

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

func InboxForSender(sender emailsending.Sender, domain emailsending.SendingDomain) Inbox {
	return Inbox{
		ID:          sender.ID,
		WorkspaceID: sender.WorkspaceID,
		Name:        strings.TrimSpace(sender.DisplayName),
		Address:     sender.FromAddress(domain),
	}
}

func (i Inbox) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              i.AgentID,
		WorkflowID:           i.WorkflowID,
		EnableAgentResponses: i.EnableAgentResponses,
		EnableWorkflow:       i.EnableWorkflow,
		EnableAnalysis:       i.EnableAnalysis,
		EnableAutoStaging:    i.EnableAutoStaging,
		EnableAutoMemory:     i.EnableAutoMemory,
	}
}

func (i Inbox) Label() string {
	if i.Name != "" {
		return i.Name
	}
	return string(i.Address)
}

type InboxSettings struct {
	DepartmentID         *string
	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses bool
	EnableWorkflow       bool
	EnableAnalysis       bool
	EnableAutoStaging    bool
	EnableAutoMemory     bool
}

func (i *Inbox) Apply(s InboxSettings) {
	i.DepartmentID = trimmedRef(s.DepartmentID)
	i.AgentID = trimmedRef(s.AgentID)
	i.WorkflowID = trimmedRef(s.WorkflowID)
	i.PipelineID = trimmedRef(s.PipelineID)
	i.EnableAgentResponses = s.EnableAgentResponses
	i.EnableWorkflow = s.EnableWorkflow
	i.EnableAnalysis = s.EnableAnalysis
	i.EnableAutoStaging = s.EnableAutoStaging
	i.EnableAutoMemory = s.EnableAutoMemory
}

func trimmedRef(ref *string) *string {
	if ref == nil {
		return nil
	}
	v := strings.TrimSpace(*ref)
	if v == "" {
		return nil
	}
	return &v
}

type Contact struct {
	ID          string               `json:"id"`
	WorkspaceID string               `json:"workspaceId"`
	Address     emailaddress.Address `json:"address"`
	DisplayName string               `json:"displayName,omitempty"`
	LeadID      *string              `json:"leadId,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

func (c Contact) Name() string {
	if name := strings.TrimSpace(c.DisplayName); name != "" {
		return name
	}
	return string(c.Address)
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	ContactID   string `json:"contactId"`
	SenderID    string `json:"senderId"`

	Subject              string   `json:"subject,omitempty"`
	ThreadKey            string   `json:"threadKey,omitempty"`
	LastInboundMessageID string   `json:"lastInboundMessageId,omitempty"`
	References           []string `json:"references,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Thread struct {
	Subject              string
	ThreadKey            string
	LastInboundMessageID string
	References           []string
}

func (c Conversation) ThreadAfter(m InboundMessage) Thread {
	subject := NormalizeSubject(m.Subject)
	if subject == "" {
		subject = c.Subject
	}
	incoming := append(append([]string{}, m.References...), m.InReplyTo, m.MessageID)
	references := ReferenceChain(c.References, incoming...)
	key := c.ThreadKey
	if key == "" && len(references) > 0 {
		key = references[0]
	}
	last := NormalizeMessageID(m.MessageID)
	if last == "" {
		last = c.LastInboundMessageID
	}
	return Thread{Subject: subject, ThreadKey: key, LastInboundMessageID: last, References: references}
}

type FindOrCreateContactInput struct {
	WorkspaceID string
	Address     emailaddress.Address
	DisplayName string
	LeadID      *string
}

type FindOrCreateConversationInput struct {
	WorkspaceID string
	ContactID   string
	SenderID    string
}
