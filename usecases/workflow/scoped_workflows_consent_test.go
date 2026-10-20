package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/workflow"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type consentHolders map[string]bool

func (h consentHolders) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	if !h[userID] {
		return resource == "leads" && (action == "read" || action == "update")
	}
	return resource == "leads"
}

type createWorkflowStub struct{ *workflowCalls }

func (c createWorkflowStub) Execute(_ context.Context, wf *workflow.Workflow) (*workflow.Workflow, error) {
	c.record("create", wf.Name)
	return wf, nil
}

func grantStep(purpose string) workflow.Node {
	return workflow.Node{ID: "grant", Type: workflow.NodeTypeActionConsent, Config: map[string]interface{}{
		workflow.ConsentModeKey: workflow.ConsentModeGrant, workflow.ConsentPurposeKey: purpose}}
}

func withdrawStep() workflow.Node {
	return workflow.Node{ID: "withdraw", Type: workflow.NodeTypeActionConsent, Config: map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}}
}

func consentFixture(checker workspace.PermissionChecker) (workflow.ScopedWorkflowsUseCase, *workflowCalls) {
	calls := &workflowCalls{}
	stored := getWorkflowStub{
		"w-grant": {ID: "w-grant", WorkspaceID: "ws1", DepartmentID: "d-sales", Graph: workflow.Graph{Nodes: []workflow.Node{grantStep("ofertas")}}},
		"w-plain": {ID: "w-plain", WorkspaceID: "ws1", DepartmentID: "d-sales", Graph: workflow.Graph{Nodes: []workflow.Node{withdrawStep()}}},
	}
	return NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Create: createWorkflowStub{calls}, Get: stored, Update: updateWorkflowStub{calls}, Delete: deleteWorkflowStub{calls},
		Activate: activateWorkflowStub{statusWorkflowStub{calls, "activate"}}, Pause: statusWorkflowStub{calls, "pause"},
		Permissions: checker,
	}), calls
}

func scopeOf(userID string) workflow.Scope {
	return workflow.Scope{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}, Requester: workspace.Requester{UserID: userID}}
}

var consentPermissions = consentHolders{"u-grantor": true}

func TestCreatingAWorkflowThatGrantsConsentNeedsTheConsentPermission(t *testing.T) {
	uc, calls := consentFixture(consentPermissions)
	grants := &workflow.Workflow{Name: "optin", Graph: workflow.Graph{Nodes: []workflow.Node{grantStep("")}}}
	if _, err := uc.Create(context.Background(), scopeOf("u-plain"), grants); !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		t.Fatalf("err = %v", err)
	}
	if len(calls.calls) != 0 {
		t.Fatalf("created without the permission: %v", calls.calls)
	}
	if _, err := uc.Create(context.Background(), scopeOf("u-grantor"), grants); err != nil {
		t.Fatal(err)
	}
	plain := &workflow.Workflow{Name: "saida", Graph: workflow.Graph{Nodes: []workflow.Node{withdrawStep()}}}
	if _, err := uc.Create(context.Background(), scopeOf("u-plain"), plain); err != nil {
		t.Fatal(err)
	}
	if got := calls.calls; len(got) != 2 || got[0] != "create:optin" || got[1] != "create:saida" {
		t.Fatalf("calls = %v", got)
	}
}

func TestUpdatingAWorkflowNeedsTheConsentPermissionOnlyForANewOrChangedGrant(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		nodes   []workflow.Node
		refused bool
	}{
		{"the stored grant kept as it was", "w-grant", []workflow.Node{grantStep("ofertas"), withdrawStep()}, false},
		{"the grant purpose changed", "w-grant", []workflow.Node{grantStep("cobranca")}, true},
		{"a grant added", "w-plain", []workflow.Node{withdrawStep(), grantStep("")}, true},
		{"the grant removed", "w-grant", []workflow.Node{withdrawStep()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, calls := consentFixture(consentPermissions)
			_, err := uc.Update(context.Background(), scopeOf("u-plain"), tc.id, &workflow.Workflow{Name: "x", Graph: workflow.Graph{Nodes: tc.nodes}}, 1)
			if tc.refused {
				if !errors.Is(err, workspace.ErrConsentGrantForbidden) || len(calls.calls) != 0 {
					t.Fatalf("err %v calls %v", err, calls.calls)
				}
				if _, err := uc.Update(context.Background(), scopeOf("u-grantor"), tc.id, &workflow.Workflow{Name: "x", Graph: workflow.Graph{Nodes: tc.nodes}}, 1); err != nil {
					t.Fatalf("a grantor was refused: %v", err)
				}
				return
			}
			if err != nil || len(calls.calls) != 1 {
				t.Fatalf("err %v calls %v", err, calls.calls)
			}
		})
	}
}

func TestActivatingAWorkflowThatGrantsConsentAlwaysNeedsTheConsentPermission(t *testing.T) {
	uc, calls := consentFixture(consentPermissions)
	if _, err := uc.Activate(context.Background(), scopeOf("u-plain"), "w-grant"); !errors.Is(err, workspace.ErrConsentGrantForbidden) || len(calls.calls) != 0 {
		t.Fatalf("err %v calls %v", err, calls.calls)
	}
	if _, err := uc.Activate(context.Background(), scopeOf("u-plain"), "w-plain"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Activate(context.Background(), scopeOf("u-grantor"), "w-grant"); err != nil {
		t.Fatal(err)
	}
	if got := calls.calls; len(got) != 2 || got[0] != "activate:w-plain" || got[1] != "activate:w-grant" {
		t.Fatalf("calls = %v", got)
	}
}

func TestAWorkflowGrantFailsClosedWithoutAPermissionChecker(t *testing.T) {
	uc, calls := consentFixture(nil)
	if _, err := uc.Activate(context.Background(), scopeOf("u-grantor"), "w-grant"); !errors.Is(err, workspace.ErrConsentGrantForbidden) || len(calls.calls) != 0 {
		t.Fatalf("err %v calls %v", err, calls.calls)
	}
	if _, err := uc.Activate(context.Background(), scopeOf("u-grantor"), "w-plain"); err != nil {
		t.Fatalf("a workflow without a grant needs no checker: %v", err)
	}
}
