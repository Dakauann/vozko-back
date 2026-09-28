package conversation

import "strings"

type ChannelAutomation struct {
	AgentID              *string
	WorkflowID           *string
	EnableAgentResponses bool
	EnableWorkflow       bool
	EnableAnalysis       bool
	EnableAutoStaging    bool
	EnableAutoMemory     bool
	Disclosure           string
}

func (c ChannelAutomation) RunsAnalysis() bool {
	return c.EnableAnalysis || c.EnableAutoStaging || c.EnableAutoMemory
}

func (c ChannelAutomation) RunsWorkflows() bool { return c.EnableWorkflow }

func (c ChannelAutomation) HasAgent() bool {
	return c.AgentID != nil && strings.TrimSpace(*c.AgentID) != ""
}

func (c ChannelAutomation) WorkflowRef() string {
	if c.WorkflowID == nil {
		return ""
	}
	return strings.TrimSpace(*c.WorkflowID)
}

func AutomationAllowed(conversationOverride *bool) bool {
	return conversationOverride == nil || *conversationOverride
}
