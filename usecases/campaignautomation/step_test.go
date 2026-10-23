package campaignautomation

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/campaign"
	"vozko/domain/workflow"
)

type fakeWorkflows struct {
	byID map[string]*workflow.Workflow
	err  error
}

func (f fakeWorkflows) Execute(id string) (*workflow.Workflow, error) {
	if f.err != nil {
		return nil, f.err
	}
	if wf, ok := f.byID[id]; ok {
		return wf, nil
	}
	return nil, workflow.ErrWorkflowNotFound
}

type fakeAgents struct {
	byID map[string]*agent.Agent
	err  error
}

func (f fakeAgents) Execute(id string) (*agent.Agent, error) {
	if f.err != nil {
		return nil, f.err
	}
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, agent.ErrAgentNotFound
}

func needsSchoolWorkflow(workspaceID string) *workflow.Workflow {
	return &workflow.Workflow{ID: "wf-1", WorkspaceID: workspaceID, Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n1", Config: map[string]interface{}{"text": "Olá da {{campvars.escola}}"}},
	}}}
}

func needsSchoolAgent(workspaceID string) *agent.Agent {
	return &agent.Agent{ID: "agent-1", WorkspaceID: workspaceID, Variables: []agent.AgentVariable{{Name: "escola"}}}
}

func withSchool() []campaign.EntryMetadata {
	return []campaign.EntryMetadata{{Label: "lead 1", Metadata: map[string]any{"escola": "Prisma"}}}
}

func withoutSchool() []campaign.EntryMetadata {
	return []campaign.EntryMetadata{{Label: "lead 1"}}
}

func newStep(t *testing.T, workflows Workflows, agents Agents) *Step {
	t.Helper()
	step, err := NewStep(workflows, agents)
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	return step
}

func TestTheStepRefusesToBuildWithoutItsLookups(t *testing.T) {
	if _, err := NewStep(nil, fakeAgents{}); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("without workflows = %v", err)
	}
	if _, err := NewStep(fakeWorkflows{}, nil); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("without agents = %v", err)
	}
	var step *Step
	if err := step.Check("ws-1", campaign.Automation{}, nil); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("a nil step = %v", err)
	}
}

func TestCheck(t *testing.T) {
	workflows := fakeWorkflows{byID: map[string]*workflow.Workflow{"wf-1": needsSchoolWorkflow("ws-1"), "wf-other": needsSchoolWorkflow("ws-2")}}
	agents := fakeAgents{byID: map[string]*agent.Agent{"agent-1": needsSchoolAgent("ws-1"), "agent-other": needsSchoolAgent("ws-2")}}
	cases := []struct {
		name       string
		workflows  Workflows
		agents     Agents
		automation campaign.Automation
		entries    []campaign.EntryMetadata
		want       error
	}{
		{name: "no automation", automation: campaign.Automation{}, entries: withoutSchool()},
		{name: "a workflow whose variables are filled", automation: campaign.Automation{WorkflowID: "wf-1"}, entries: withSchool()},
		{name: "a missing workflow variable", automation: campaign.Automation{WorkflowID: "wf-1"}, entries: withoutSchool(), want: campaign.ErrWorkflowVarsMissing},
		{name: "a workflow that does not exist", automation: campaign.Automation{WorkflowID: "wf-x"}, entries: withSchool(), want: campaign.ErrWorkflowNotFound},
		{name: "a workflow lookup that fails", workflows: fakeWorkflows{err: errors.New("db down")}, automation: campaign.Automation{WorkflowID: "wf-1"}, entries: withSchool(), want: campaign.ErrWorkflowNotFound},
		{name: "another workspace's workflow", automation: campaign.Automation{WorkflowID: "wf-other"}, entries: withSchool(), want: campaign.ErrWorkflowForbidden},
		{name: "an agent whose variables are filled", automation: campaign.Automation{AgentID: "agent-1"}, entries: withSchool()},
		{name: "a missing agent variable", automation: campaign.Automation{AgentID: "agent-1"}, entries: withoutSchool(), want: campaign.ErrAgentVarsMissing},
		{name: "an agent lookup that fails refuses", agents: fakeAgents{err: errors.New("db down")}, automation: campaign.Automation{AgentID: "agent-1"}, entries: withSchool(), want: campaign.ErrAgentNotFound},
		{name: "an agent that does not exist", automation: campaign.Automation{AgentID: "agent-x"}, entries: withSchool(), want: campaign.ErrAgentNotFound},
		{name: "another workspace's agent", automation: campaign.Automation{AgentID: "agent-other"}, entries: withSchool(), want: campaign.ErrAgentNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, a := tc.workflows, tc.agents
			if w == nil {
				w = workflows
			}
			if a == nil {
				a = agents
			}
			if err := newStep(t, w, a).Check("ws-1", tc.automation, tc.entries); !errors.Is(err, tc.want) {
				t.Fatalf("Check = %v, want %v", err, tc.want)
			}
		})
	}
}

type countingChecker struct {
	err   error
	calls int
}

func (c *countingChecker) Check(string, campaign.Automation, []campaign.EntryMetadata) error {
	c.calls++
	return c.err
}

func TestRequireRunsTheStepOnlyWhenTheCampaignHasAutomation(t *testing.T) {
	if err := Require(nil, "ws-1", campaign.Automation{}, nil); err != nil {
		t.Fatalf("a campaign without automation needs no step, got %v", err)
	}
	if err := Require(nil, "ws-1", campaign.Automation{WorkflowID: "wf-1"}, nil); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Require without the step = %v, want ErrAutomationUnavailable", err)
	}
	refusing := &countingChecker{err: campaign.ErrWorkflowVarsMissing}
	if err := Require(refusing, "ws-1", campaign.Automation{WorkflowID: "wf-1"}, nil); !errors.Is(err, campaign.ErrWorkflowVarsMissing) || refusing.calls != 1 {
		t.Fatalf("Require = %v calls %d, want the step refusal", err, refusing.calls)
	}
	var nilStep *Step
	if err := Require(nilStep, "ws-1", campaign.Automation{AgentID: "agent-1"}, nil); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Require with a nil step = %v, want ErrAutomationUnavailable", err)
	}
}

func storedEntries(entries ...campaign.EntryMetadata) (StoredEntries, *int) {
	visited := 0
	return func(visit func(campaign.EntryMetadata) error) error {
		for _, entry := range entries {
			visited++
			if err := visit(entry); err != nil {
				return err
			}
		}
		return nil
	}, &visited
}

func TestRequireStoredChecksTheSavedEntriesOnlyWhenTheAutomationChanges(t *testing.T) {
	step := newStep(t,
		fakeWorkflows{byID: map[string]*workflow.Workflow{"wf-1": needsSchoolWorkflow("ws-1")}},
		fakeAgents{byID: map[string]*agent.Agent{"agent-1": needsSchoolAgent("ws-1")}},
	)
	school := campaign.EntryMetadata{Label: "lead 1", Metadata: map[string]any{"escola": "Prisma"}}
	noSchool := campaign.EntryMetadata{Label: "lead 2"}
	workflowAutomation := campaign.Automation{WorkflowID: "wf-1", EnableWorkflow: true}
	agentAutomation := campaign.Automation{AgentID: "agent-1", EnableAgentResponses: true}
	cases := []struct {
		name   string
		before campaign.Automation
		after  campaign.Automation
		saved  []campaign.EntryMetadata
		want   error
	}{
		{name: "an edit that keeps the automation reads no entry", before: workflowAutomation, after: workflowAutomation, saved: []campaign.EntryMetadata{noSchool}},
		{name: "turning the automation off reads no entry", before: workflowAutomation, after: campaign.Automation{}, saved: []campaign.EntryMetadata{noSchool}},
		{name: "a new workflow whose variables every saved entry has", before: campaign.Automation{}, after: workflowAutomation, saved: []campaign.EntryMetadata{school, school}},
		{name: "a new workflow a saved entry cannot fill", before: campaign.Automation{}, after: workflowAutomation, saved: []campaign.EntryMetadata{school, noSchool}, want: campaign.ErrWorkflowVarsMissing},
		{name: "a new agent a saved entry cannot fill", before: workflowAutomation, after: agentAutomation, saved: []campaign.EntryMetadata{noSchool}, want: campaign.ErrAgentVarsMissing},
		{name: "a new automation that does not exist", before: campaign.Automation{}, after: campaign.Automation{WorkflowID: "wf-x"}, want: campaign.ErrWorkflowNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored, _ := storedEntries(tc.saved...)
			if err := RequireStored(step, "ws-1", tc.before, tc.after, stored); !errors.Is(err, tc.want) {
				t.Fatalf("RequireStored = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRequireStoredStopsAtTheFirstEntryThatCannotBeFilled(t *testing.T) {
	step := newStep(t, fakeWorkflows{byID: map[string]*workflow.Workflow{"wf-1": needsSchoolWorkflow("ws-1")}}, fakeAgents{})
	noSchool := campaign.EntryMetadata{Label: "lead 1"}
	stored, visited := storedEntries(noSchool, noSchool, noSchool)
	if err := RequireStored(step, "ws-1", campaign.Automation{}, campaign.Automation{WorkflowID: "wf-1"}, stored); !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("RequireStored = %v, want ErrWorkflowVarsMissing", err)
	}
	if *visited != 1 {
		t.Fatalf("visited %d entries, want the walk to stop at the first refusal", *visited)
	}
}

func TestRequireStoredResolvesTheAutomationOnceForEveryEntry(t *testing.T) {
	workflows := &countingWorkflows{wf: needsSchoolWorkflow("ws-1")}
	step := newStep(t, workflows, fakeAgents{})
	school := campaign.EntryMetadata{Label: "lead 1", Metadata: map[string]any{"escola": "Prisma"}}
	stored, _ := storedEntries(school, school, school, school)
	if err := RequireStored(step, "ws-1", campaign.Automation{}, campaign.Automation{WorkflowID: "wf-1"}, stored); err != nil {
		t.Fatalf("RequireStored = %v", err)
	}
	if workflows.calls != 1 {
		t.Fatalf("looked the workflow up %d times, want once", workflows.calls)
	}
}

func TestRequireStoredRefusesAChangedAutomationItCannotVerify(t *testing.T) {
	stored, _ := storedEntries()
	after := campaign.Automation{WorkflowID: "wf-1"}
	if err := RequireStored(nil, "ws-1", campaign.Automation{}, after, stored); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("without the rules = %v, want ErrAutomationUnavailable", err)
	}
	step := newStep(t, fakeWorkflows{byID: map[string]*workflow.Workflow{"wf-1": needsSchoolWorkflow("ws-1")}}, fakeAgents{})
	if err := RequireStored(step, "ws-1", campaign.Automation{}, after, nil); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("without the saved entries = %v, want ErrAutomationUnavailable", err)
	}
	failing := func(func(campaign.EntryMetadata) error) error { return errors.New("db down") }
	if err := RequireStored(step, "ws-1", campaign.Automation{}, after, failing); err == nil {
		t.Fatal("a failed read of the saved entries must refuse the edit")
	}
}

type countingWorkflows struct {
	wf    *workflow.Workflow
	calls int
}

func (c *countingWorkflows) Execute(string) (*workflow.Workflow, error) {
	c.calls++
	return c.wf, nil
}

func TestRuleMissingNamesEveryKeyTheAutomationNeedsOnce(t *testing.T) {
	needsClass := &agent.Agent{ID: "agent-2", WorkspaceID: "ws-1", Variables: []agent.AgentVariable{{Name: "escola"}, {Name: "turma"}}}
	step := newStep(t, fakeWorkflows{byID: map[string]*workflow.Workflow{"wf-1": needsSchoolWorkflow("ws-1")}}, fakeAgents{byID: map[string]*agent.Agent{"agent-2": needsClass}})
	rule, err := step.Rule("ws-1", campaign.Automation{WorkflowID: "wf-1", AgentID: "agent-2"})
	if err != nil {
		t.Fatalf("Rule: %v", err)
	}
	if got := rule.Missing(nil); !reflect.DeepEqual(got, []string{"escola", "turma"}) {
		t.Fatalf("no metadata = %v", got)
	}
	if got := rule.Missing(map[string]any{"escola": "Prisma", "turma": " "}); !reflect.DeepEqual(got, []string{"turma"}) {
		t.Fatalf("a blank key = %v", got)
	}
	if !rule.Requires() {
		t.Fatal("a rule with keys requires metadata")
	}
	none, err := step.Rule("ws-1", campaign.Automation{})
	if err != nil {
		t.Fatalf("Rule without automation: %v", err)
	}
	if none.Requires() || len(none.Missing(nil)) != 0 {
		t.Fatalf("no automation requires nothing: %v", none.Missing(nil))
	}
}
