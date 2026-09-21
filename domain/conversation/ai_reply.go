package conversation

import "vozko/domain/shared"

type AIReplyRequest struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType

	AgentID               string
	AgentResponsesEnabled bool
	AutomationEnabled     *bool

	Text string

	LeadID *string
}
