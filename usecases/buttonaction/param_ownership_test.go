package buttonaction_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

var errNotStored = errors.New("not stored")

type ownedWorkflows map[string]*workflow.Workflow

func (o ownedWorkflows) FindByID(id string) (*workflow.Workflow, error) {
	if w, ok := o[id]; ok {
		return w, nil
	}
	return nil, errNotStored
}

func (o ownedWorkflows) FindVersion(workflowID string, version int) (*workflow.WorkflowVersion, error) {
	if w, ok := o[workflowID+"@published"]; ok && w.PublishedVersion != nil && *w.PublishedVersion == version {
		graph := w.Graph
		return &workflow.WorkflowVersion{WorkflowID: workflowID, Version: version, Type: workflow.WorkflowTypeMessages, Graph: &graph}, nil
	}
	return nil, errNotStored
}

func graphWith(triggers ...workflow.NodeType) workflow.Graph {
	nodes := []workflow.Node{{ID: "end", Type: workflow.NodeTypeEnd}}
	for _, t := range triggers {
		nodes = append(nodes, workflow.Node{ID: string(t), Type: t})
	}
	return workflow.Graph{Nodes: nodes}
}

func publishedWorkflows() ownedWorkflows {
	one := 1
	return ownedWorkflows{
		"wf-1":              {ID: "wf-1", WorkspaceID: ws, Graph: graphWith(workflow.NodeTypeTriggerMessageReceived)},
		"wf-other":          {ID: "wf-other", WorkspaceID: "ws-2", Graph: graphWith(workflow.NodeTypeTriggerMessageReceived)},
		"wf-first":          {ID: "wf-first", WorkspaceID: ws, Graph: graphWith(workflow.NodeTypeTriggerAgentCall, workflow.NodeTypeTriggerFirstMessage)},
		"wf-hook":           {ID: "wf-hook", WorkspaceID: ws, Graph: graphWith(workflow.NodeTypeTriggerWebhook)},
		"wf-live":           {ID: "wf-live", WorkspaceID: ws, Status: workflow.WorkflowStatusActive, PublishedVersion: &one, Graph: graphWith(workflow.NodeTypeTriggerMessageReceived)},
		"wf-live@published": {PublishedVersion: &one, Graph: graphWith(workflow.NodeTypeTriggerAgentCall)},
		"wf-lost":           {ID: "wf-lost", WorkspaceID: ws, Status: workflow.WorkflowStatusActive, PublishedVersion: &one, Graph: graphWith(workflow.NodeTypeTriggerMessageReceived)},
	}
}

type ownedCallLists map[string]*calllist.List

func (o ownedCallLists) Get(_ context.Context, workspaceID, id string) (*calllist.List, error) {
	if l, ok := o[workspaceID+"|"+id]; ok {
		return l, nil
	}
	return nil, errNotStored
}

type ownedFields struct {
	defs []*customfield.Definition
	err  error
}

func (o ownedFields) ListByObject(workspaceID string, _ customfield.ObjectType) ([]*customfield.Definition, error) {
	if workspaceID != ws {
		return nil, nil
	}
	return o.defs, o.err
}

type ownedPipelines map[string]*pipeline.Pipeline

func (o ownedPipelines) GetByID(_, id string) (*pipeline.Pipeline, error) {
	if p, ok := o[id]; ok {
		return p, nil
	}
	return nil, errNotStored
}

type ownedStages map[string]*stage.Stage

func (o ownedStages) FindByID(id string) (*stage.Stage, error) {
	if s, ok := o[id]; ok {
		return s, nil
	}
	return nil, errNotStored
}

type ownedDepartments map[string]*wd.Department

func (o ownedDepartments) GetDepartmentByID(id string) (*wd.Department, error) {
	if d, ok := o[id]; ok {
		return d, nil
	}
	return nil, errNotStored
}

type usableAutomations map[conversation.Automation]bool

func (u usableAutomations) Usable(workspaceID string, a conversation.Automation) error {
	if workspaceID == ws && u[a] {
		return nil
	}
	return conversation.ErrAutomationUnusable
}

type ownedMembers map[string]*workspace.Member

func (o ownedMembers) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if m, ok := o[workspaceID+"|"+userID]; ok {
		return m, nil
	}
	return nil, errNotStored
}

func ownershipFixture(t *testing.T, fields ownedFields) *WorkspaceOwnership {
	t.Helper()
	o, err := NewWorkspaceOwnership(OwnershipDeps{
		Workflows: publishedWorkflows(),
		CallLists: ownedCallLists{ws + "|list-1": {ID: "list-1"}},
		Fields:    fields,
		Pipelines: ownedPipelines{"pipe-1": {ID: "pipe-1", WorkspaceID: ws}},
		Stages: ownedStages{
			"stage-1": {ID: "stage-1", WorkspaceID: ws, PipelineID: "pipe-1"},
			"stage-2": {ID: "stage-2", WorkspaceID: ws, PipelineID: "pipe-2"},
		},
		Departments: ownedDepartments{"dep-1": {ID: "dep-1", WorkspaceID: ws}, "dep-other": {ID: "dep-other", WorkspaceID: "ws-2"}},
		Members:     ownedMembers{ws + "|user-ana": {UserID: "user-ana", WorkspaceID: ws}},
		Automations: usableAutomations{
			{Kind: conversation.AutomationAgent, ID: "agent-1"}: true,
			{Kind: conversation.AutomationWorkflow, ID: "wf-1"}: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func interestField() ownedFields {
	return ownedFields{defs: []*customfield.Definition{
		{Key: "cidade", Type: customfield.TypeText},
		{Key: "interesse", Type: customfield.TypeSelect, Options: []string{"sim", "nao"}},
	}}
}

func TestEveryActionParameterMustBelongToTheWorkspace(t *testing.T) {
	o := ownershipFixture(t, interestField())
	cases := []struct {
		name  string
		kind  buttonaction.Kind
		p     buttonaction.Params
		owned bool
	}{
		{"an opt out has nothing to own", buttonaction.KindOptOut, buttonaction.Params{}, true},
		{"a team of this workspace", buttonaction.KindHandOff, buttonaction.Params{DepartmentID: "dep-1"}, true},
		{"a team of another workspace", buttonaction.KindHandOff, buttonaction.Params{DepartmentID: "dep-other"}, false},
		{"a member of this workspace", buttonaction.KindHandOff, buttonaction.Params{UserID: "user-ana"}, true},
		{"a stranger", buttonaction.KindHandOff, buttonaction.Params{UserID: "user-x"}, false},
		{"an agent that can answer", buttonaction.KindDelegate, buttonaction.Params{AgentID: "agent-1"}, true},
		{"an agent that cannot answer", buttonaction.KindDelegate, buttonaction.Params{AgentID: "agent-off"}, false},
		{"a workflow that takes conversations", buttonaction.KindDelegate, buttonaction.Params{WorkflowID: "wf-1"}, true},
		{"a workflow that cannot take conversations", buttonaction.KindDelegate, buttonaction.Params{WorkflowID: "wf-other"}, false},
		{"a workflow of this workspace", buttonaction.KindStartWorkflow, buttonaction.Params{WorkflowID: "wf-1"}, true},
		{"a workflow of another workspace", buttonaction.KindStartWorkflow, buttonaction.Params{WorkflowID: "wf-other"}, false},
		{"a call list of this workspace", buttonaction.KindCallList, buttonaction.Params{CallListID: "list-1"}, true},
		{"an unknown call list", buttonaction.KindCallList, buttonaction.Params{CallListID: "list-9"}, false},
		{"an option of a select field", buttonaction.KindClassify, buttonaction.Params{FieldKey: "interesse", Option: "sim"}, true},
		{"an option the field does not offer", buttonaction.KindClassify, buttonaction.Params{FieldKey: "interesse", Option: "talvez"}, false},
		{"a field that is not a select", buttonaction.KindClassify, buttonaction.Params{FieldKey: "cidade", Option: "sim"}, false},
		{"a stage of the pipeline", buttonaction.KindCreateDeal, buttonaction.Params{PipelineID: "pipe-1", StageID: "stage-1"}, true},
		{"a stage of another pipeline", buttonaction.KindCreateDeal, buttonaction.Params{PipelineID: "pipe-1", StageID: "stage-2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := o.Check(context.Background(), ws, tc.kind, tc.p)
			if tc.owned && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.owned && !errors.Is(err, buttonaction.ErrParamsNotInWorkspace) {
				t.Fatalf("err = %v, want ErrParamsNotInWorkspace", err)
			}
		})
	}
}

func TestAButtonStartsOnlyAWorkflowWithAMessageTriggerInTheVersionThatRuns(t *testing.T) {
	o := ownershipFixture(t, interestField())
	cases := []struct {
		name string
		id   string
		want error
	}{
		{"a draft with a message trigger", "wf-1", nil},
		{"a first message trigger beside an agent call", "wf-first", nil},
		{"only a webhook trigger", "wf-hook", workflow.ErrWorkflowNeedsMessageTrigger},
		{"the published version has no message trigger even if the draft does", "wf-live", workflow.ErrWorkflowNeedsMessageTrigger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := o.Check(context.Background(), ws, buttonaction.KindStartWorkflow, buttonaction.Params{WorkflowID: tc.id})
			if tc.want == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.want != nil && (!errors.Is(err, tc.want) || buttonaction.ErrorCode(err) != workflow.NeedsMessageTriggerCode) {
				t.Fatalf("err = %v code = %q, want %v", err, buttonaction.ErrorCode(err), tc.want)
			}
		})
	}
	if err := o.Check(context.Background(), ws, buttonaction.KindStartWorkflow, buttonaction.Params{WorkflowID: "wf-lost"}); err == nil || errors.Is(err, buttonaction.ErrParamsNotInWorkspace) {
		t.Fatalf("an unreadable published version must fail closed, got %v", err)
	}
	if err := o.Check(context.Background(), ws, buttonaction.KindDelegate, buttonaction.Params{WorkflowID: "wf-1"}); err != nil {
		t.Fatalf("a delegation is not a start and keeps its own rule: %v", err)
	}
}

func TestOwnershipFailsClosed(t *testing.T) {
	if _, err := NewWorkspaceOwnership(OwnershipDeps{}); !errors.Is(err, buttonaction.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	o := ownershipFixture(t, ownedFields{err: errors.New("database down")})
	if err := o.Check(context.Background(), ws, buttonaction.KindClassify, buttonaction.Params{FieldKey: "interesse", Option: "sim"}); err == nil || errors.Is(err, buttonaction.ErrParamsNotInWorkspace) {
		t.Fatalf("a read failure must be an error, got %v", err)
	}
	if err := o.Check(context.Background(), ws, buttonaction.Kind("teleport"), buttonaction.Params{}); !errors.Is(err, buttonaction.ErrKindUnknown) {
		t.Fatalf("got %v", err)
	}
}
