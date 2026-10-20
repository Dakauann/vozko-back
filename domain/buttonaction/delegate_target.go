package buttonaction

import "vozko/domain/conversation"

func (p Params) Automation() conversation.Automation {
	if p.AgentID != "" {
		return conversation.Automation{Kind: conversation.AutomationAgent, ID: p.AgentID}
	}
	return conversation.Automation{Kind: conversation.AutomationWorkflow, ID: p.WorkflowID}
}
