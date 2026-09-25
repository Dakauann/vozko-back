package copilottools

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	wd "vozko/domain/workspace/workspace_department"
)

type fakeListDepartments struct{ depts []wd.Department }

func (f fakeListDepartments) Execute(string) ([]wd.Department, error) { return f.depts, nil }

type fakeListAgents struct {
	calls int
	got   agent.ListAgentsInput
}

func (f *fakeListAgents) Execute(in agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	f.calls++
	f.got = in
	return &shared.PaginatedResult[*agent.AgentListItem]{}, nil
}

func strandedContext() copilot.Context {
	return copilot.Context{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{WorkspaceHasDepartments: true}}
}

func TestStrandedMemberSeesNoDepartments(t *testing.T) {
	tool := NewListDepartmentsTool(fakeListDepartments{depts: []wd.Department{{ID: "d1"}, {ID: "d2"}}})
	res := tool.Execute(context.Background(), strandedContext(), nil)
	data := res.Data.(map[string]interface{})
	// The old []string scope read "no departments" as "every department" and listed them all.
	if data["total"] != 0 {
		t.Fatalf("total = %v, want 0", data["total"])
	}
}

func TestStrandedMemberCannotReadAnAgent(t *testing.T) {
	a := boundAgent()
	a.DepartmentID = "d1"
	res := NewGetAgentTool(fakeGetAgent{a: a}).Execute(context.Background(), strandedContext(), map[string]interface{}{"id": "ag-1"})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status = %v, want denied", res.Status)
	}
}

func TestStrandedMemberListsNoAgentsWithoutQuerying(t *testing.T) {
	list := &fakeListAgents{}
	res := NewListAgentsTool(list).Execute(context.Background(), strandedContext(), nil)
	if res.Status != copilot.StatusOK || list.calls != 0 {
		t.Fatalf("status = %v calls = %d, want an empty ok without a query", res.Status, list.calls)
	}
}

func TestDepartmentMemberListsOnlyOwnAgents(t *testing.T) {
	list := &fakeListAgents{}
	cc := copilot.Context{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{DepartmentIDs: []string{"d2"}, WorkspaceHasDepartments: true}}
	NewListAgentsTool(list).Execute(context.Background(), cc, nil)
	if len(list.got.DepartmentIDs) != 1 || list.got.DepartmentIDs[0] != "d2" {
		t.Fatalf("DepartmentIDs = %v, want [d2]", list.got.DepartmentIDs)
	}
}

func TestMissingScopeDeniesInsteadOfWidening(t *testing.T) {
	res := NewGetAgentTool(fakeGetAgent{a: boundAgent()}).Execute(context.Background(), copilot.Context{WorkspaceID: "ws-1"}, map[string]interface{}{"id": "ag-1"})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status = %v, want denied when no department filter reached the tool", res.Status)
	}
}
