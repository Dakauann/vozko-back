package campaignautomation

import (
	"fmt"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/campaign"
	"vozko/domain/workflow"
)

type Workflows interface {
	Execute(workflowID string) (*workflow.Workflow, error)
}

type Agents interface {
	Execute(agentID string) (*agent.Agent, error)
}

type Step struct {
	workflows Workflows
	agents    Agents
}

func NewStep(workflows Workflows, agents Agents) (*Step, error) {
	if workflows == nil || agents == nil {
		return nil, campaign.ErrAutomationUnavailable
	}
	return &Step{workflows: workflows, agents: agents}, nil
}

func (s *Step) Check(workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error {
	if s == nil || s.workflows == nil || s.agents == nil {
		return campaign.ErrAutomationUnavailable
	}
	if err := s.checkWorkflow(workspaceID, strings.TrimSpace(a.WorkflowID), entries); err != nil {
		return err
	}
	return s.checkAgent(workspaceID, strings.TrimSpace(a.AgentID), entries)
}

func (s *Step) checkWorkflow(workspaceID, workflowID string, entries []campaign.EntryMetadata) error {
	if workflowID == "" {
		return nil
	}
	wf, err := s.workflows.Execute(workflowID)
	if err != nil {
		return fmt.Errorf("%w: %w", campaign.ErrWorkflowNotFound, err)
	}
	if wf == nil {
		return campaign.ErrWorkflowNotFound
	}
	if wf.WorkspaceID != workspaceID {
		return campaign.ErrWorkflowForbidden
	}
	return campaign.RequireWorkflowVars(workflow.ExtractCampaignVars(wf.Graph), entries)
}

func (s *Step) checkAgent(workspaceID, agentID string, entries []campaign.EntryMetadata) error {
	if agentID == "" {
		return nil
	}
	found, err := s.agents.Execute(agentID)
	if err != nil {
		return fmt.Errorf("%w: %w", campaign.ErrAgentNotFound, err)
	}
	if found == nil || found.WorkspaceID != workspaceID {
		return campaign.ErrAgentNotFound
	}
	return campaign.RequireAgentVars(found, entries)
}

type Checker interface {
	Check(workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error
}

func Require(step Checker, workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error {
	if !a.Configured() {
		return nil
	}
	if step == nil {
		return campaign.ErrAutomationUnavailable
	}
	return step.Check(workspaceID, a, entries)
}
