package campaign

import "strings"

// AutomationMode is which automation, if any, attends a reply to a campaign.
type AutomationMode string

const (
	// AutomationNone means nothing answers. The transcript is still written and
	// analysis may still run; no agent replies and no workflow is triggered.
	AutomationNone AutomationMode = "none"
	// AutomationWorkflow means the campaign's own workflow attends, and the
	// agent is suppressed even if one is configured.
	AutomationWorkflow AutomationMode = "workflow"
	// AutomationAgent means the campaign's AI agent attends.
	AutomationAgent AutomationMode = "agent"
)

// Automation is a campaign's answer to "who handles the reply".
//
// The four fields are stored identically by both channels, because an operator
// filling in the automation panel is answering the same question regardless of
// how the message physically leaves the building.
type Automation struct {
	AgentID              string
	WorkflowID           string
	EnableAgentResponses bool
	EnableWorkflow       bool
}

// Mode decides which automation attends, given an optional per-conversation
// operator override (nil = never toggled, inherit the campaign).
//
// Three rules, in order, and the order is the whole point:
//
//  1. An operator who switched automation off on one conversation wins over
//     everything. They did that while looking at the conversation.
//  2. A workflow beats an agent. Both replying means the customer gets two
//     answers to one message, so a campaign with both configured runs only the
//     workflow.
//  3. Anything not positively configured is off. A campaign with no workflow and
//     no agent must answer with silence, NOT fall through to whatever automation
//     the surrounding channel account happens to have enabled — that is how a
//     campaign the operator deliberately left manual starts replying by itself.
//
// A flag with no id behind it is not configuration, it is a half-filled form, so
// both halves are required before either automation can claim the reply.
func (a Automation) Mode(override *bool) AutomationMode {
	if override != nil && !*override {
		return AutomationNone
	}

	if a.EnableWorkflow && strings.TrimSpace(a.WorkflowID) != "" {
		return AutomationWorkflow
	}

	// The override can force the agent on for one conversation, which is how an
	// operator hands a manual campaign back to the AI without editing the
	// campaign and affecting everyone else in it.
	agentOn := a.EnableAgentResponses
	if override != nil {
		agentOn = *override
	}
	if agentOn && strings.TrimSpace(a.AgentID) != "" {
		return AutomationAgent
	}

	return AutomationNone
}

// RunsWorkflow and RunsAgent are the two questions callers actually ask.
func (a Automation) RunsWorkflow(override *bool) bool {
	return a.Mode(override) == AutomationWorkflow
}

func (a Automation) RunsAgent(override *bool) bool {
	return a.Mode(override) == AutomationAgent
}
