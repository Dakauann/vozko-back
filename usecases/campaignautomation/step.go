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

type Rule struct {
	workflowKeys []string
	agent        *agent.Agent
}

func (r Rule) Check(entries []campaign.EntryMetadata) error {
	if err := campaign.RequireWorkflowVars(r.workflowKeys, entries); err != nil {
		return err
	}
	if r.agent == nil {
		return nil
	}
	return campaign.RequireAgentVars(r.agent, entries)
}

func (s *Step) Check(workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error {
	rule, err := s.Rule(workspaceID, a)
	if err != nil {
		return err
	}
	return rule.Check(entries)
}

func (s *Step) Rule(workspaceID string, a campaign.Automation) (Rule, error) {
	if s == nil || s.workflows == nil || s.agents == nil {
		return Rule{}, campaign.ErrAutomationUnavailable
	}
	keys, err := s.workflowKeys(workspaceID, strings.TrimSpace(a.WorkflowID))
	if err != nil {
		return Rule{}, err
	}
	found, err := s.agent(workspaceID, strings.TrimSpace(a.AgentID))
	if err != nil {
		return Rule{}, err
	}
	return Rule{workflowKeys: keys, agent: found}, nil
}

func (s *Step) workflowKeys(workspaceID, workflowID string) ([]string, error) {
	if workflowID == "" {
		return nil, nil
	}
	wf, err := s.workflows.Execute(workflowID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", campaign.ErrWorkflowNotFound, err)
	}
	if wf == nil {
		return nil, campaign.ErrWorkflowNotFound
	}
	if wf.WorkspaceID != workspaceID {
		return nil, campaign.ErrWorkflowForbidden
	}
	return workflow.ExtractCampaignVars(wf.Graph), nil
}

func (s *Step) agent(workspaceID, agentID string) (*agent.Agent, error) {
	if agentID == "" {
		return nil, nil
	}
	found, err := s.agents.Execute(agentID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", campaign.ErrAgentNotFound, err)
	}
	if found == nil || found.WorkspaceID != workspaceID {
		return nil, campaign.ErrAgentNotFound
	}
	return found, nil
}

type Checker interface {
	Check(workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error
}

type Rules interface {
	Rule(workspaceID string, a campaign.Automation) (Rule, error)
}

type StoredEntries func(visit func(campaign.EntryMetadata) error) error

func Require(step Checker, workspaceID string, a campaign.Automation, entries []campaign.EntryMetadata) error {
	if !a.Configured() {
		return nil
	}
	if step == nil {
		return campaign.ErrAutomationUnavailable
	}
	return step.Check(workspaceID, a, entries)
}

func RequireStored(rules Rules, workspaceID string, before, after campaign.Automation, stored StoredEntries) error {
	if !after.Configured() || after == before {
		return nil
	}
	if rules == nil || stored == nil {
		return campaign.ErrAutomationUnavailable
	}
	rule, err := rules.Rule(workspaceID, after)
	if err != nil {
		return err
	}
	return stored(func(entry campaign.EntryMetadata) error {
		return rule.Check([]campaign.EntryMetadata{entry})
	})
}
