package campaign

import "strings"

type AutomationMode string

const (
	AutomationNone     AutomationMode = "none"
	AutomationWorkflow AutomationMode = "workflow"
	AutomationAgent    AutomationMode = "agent"
)

type Automation struct {
	AgentID              string
	WorkflowID           string
	EnableAgentResponses bool
	EnableWorkflow       bool
}

func (a Automation) Mode(override *bool) AutomationMode {
	if override != nil && !*override {
		return AutomationNone
	}

	if a.EnableWorkflow && strings.TrimSpace(a.WorkflowID) != "" {
		return AutomationWorkflow
	}

	agentOn := a.EnableAgentResponses
	if override != nil {
		agentOn = *override
	}
	if agentOn && strings.TrimSpace(a.AgentID) != "" {
		return AutomationAgent
	}

	return AutomationNone
}

func (a Automation) RunsWorkflow(override *bool) bool {
	return a.Mode(override) == AutomationWorkflow
}

func (a Automation) RunsAgent(override *bool) bool {
	return a.Mode(override) == AutomationAgent
}
