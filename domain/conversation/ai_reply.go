package conversation

import "vozko/domain/shared"

// AIReplyRequest is one inbound message an AI agent may answer.
//
// It is deliberately channel-neutral: a channel supplies its own configuration
// (which agent, whether the account enables agent responses, whether this
// conversation has been taken over) and never its own types.
type AIReplyRequest struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType

	// AgentID is the agent configured on the channel account/container.
	AgentID string
	// AgentResponsesEnabled is the container-level switch (the account's
	// "enable agent responses").
	AgentResponsesEnabled bool
	// AutomationEnabled is the per-conversation override. nil means "never
	// toggled", which inherits the container switch, the same contract the
	// WhatsApp entry uses, so an operator taking over one conversation silences
	// the agent there without affecting the rest.
	AutomationEnabled *bool

	// Text is the inbound message body. Empty text (a bare media message) is not
	// answered.
	Text string

	// LeadID is the CRM lead bridged to this conversation, when known. Channels
	// whose contacts may not be bridged yet (Instagram, Telegram, unofficial
	// WhatsApp groups) leave it nil, which keeps lead-scoped features, memory
	// injection and the memory tool, inert for the conversation without failing it.
	LeadID *string
}
