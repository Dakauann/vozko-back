package workflow_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

var productionPinAt = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

type everyRunVisible struct{}

func (everyRunVisible) RunVisible(shared.Person, string, string) bool { return true }

func workflowPinnedFromRun() *workflow.Workflow {
	wf := debugWorkflow("https://api.example.com", nil)
	wf.ID, wf.WorkspaceID, wf.Version = "wf-1", "ws-1", 1
	wf.Graph.FindNode("login").Pinned = &workflow.NodePin{
		Output: map[string]interface{}{"cpf": "•••• 33"}, Edge: "falha", Source: workflow.PinSourceOfRun("run-1"), PinnedAt: productionPinAt,
	}
	return wf
}

func pinWithholdingFixture(t *testing.T, rule inboxRule) (workflow.ScopedWorkflowsUseCase, *MockWorkflowRepository) {
	t.Helper()
	repo := NewMockWorkflowRepository()
	if err := repo.Create(workflowPinnedFromRun()); err != nil {
		t.Fatal(err)
	}
	return NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get:        NewGetWorkflowUseCase(repo),
		Update:     NewUpdateWorkflowUseCase(repo),
		Versions:   NewWorkflowVersionsUseCase(repo),
		PinSources: NewRunPinSources(conversationRun("whatsapp"), rule),
	}), repo
}

func viewerScope(userID string) workflow.Scope {
	return workflow.Scope{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}, Requester: workspace.Requester{UserID: userID}}
}

func TestTheDraftShowsAProductionPinOnlyToWhoCanSeeItsConversation(t *testing.T) {
	for _, tc := range runVisibilityCases() {
		if tc.runEntry != "whatsapp" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			uc, _ := pinWithholdingFixture(t, tc.rule)
			wf, err := uc.Detail(viewerScope(viewer.UserID), "wf-1")
			if err != nil {
				t.Fatal(err)
			}
			pin := wf.Graph.FindNode("login").Pinned
			if pin == nil || pin.Source != workflow.PinSourceOfRun("run-1") || pin.Edge != "falha" {
				t.Fatalf("the pin mark stays for every editor: %+v", pin)
			}
			if tc.visible && (pin.Withheld || pin.Output["cpf"] != "•••• 33") {
				t.Fatalf("a viewer of the conversation sees the pinned output: %+v", pin)
			}
			if !tc.visible && (!pin.Withheld || pin.Output != nil) {
				t.Fatalf("the output of a hidden conversation must be withheld: %+v", pin)
			}
		})
	}
}

func TestAssigningADepartmentAnswersWithTheProductionPinsWithheld(t *testing.T) {
	hidden := inboxRule{departments: []string{supportDepartment}, viewOthers: true, conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment}}}
	repo := NewMockWorkflowRepository()
	if err := repo.Create(workflowPinnedFromRun()); err != nil {
		t.Fatal(err)
	}
	uc := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get:              NewGetWorkflowUseCase(repo),
		AssignDepartment: NewAssignDepartmentUseCase(repo, &workflowDepartmentResolverStub{departmentID: "dept-2"}),
		PinSources:       NewRunPinSources(conversationRun("whatsapp"), hidden),
	})

	got, err := uc.AssignDepartment(context.Background(), viewerScope(viewer.UserID), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DepartmentID != "dept-2" {
		t.Fatalf("department = %q", got.DepartmentID)
	}
	if pin := got.Graph.FindNode("login").Pinned; pin == nil || !pin.Withheld || pin.Output != nil {
		t.Fatalf("pin = %+v, the assignment answer must not leak a hidden conversation's output", pin)
	}
	if stored := repo.workflows["wf-1"].Graph.FindNode("login").Pinned; stored.Withheld || stored.Output["cpf"] != "•••• 33" {
		t.Fatalf("the stored pin must keep its output: %+v", stored)
	}
}

func TestAssigningADepartmentNeedsAccessToTheWorkflowFirst(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(workflowPinnedFromRun()); err != nil {
		t.Fatal(err)
	}
	uc := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get:              NewGetWorkflowUseCase(repo),
		AssignDepartment: NewAssignDepartmentUseCase(repo, &workflowDepartmentResolverStub{departmentID: "dept-2"}),
	})
	scope := viewerScope(viewer.UserID)
	scope.WorkspaceID = "ws-other"
	if _, err := uc.AssignDepartment(context.Background(), scope, "wf-1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v", err)
	}
	if repo.workflows["wf-1"].DepartmentID != "" {
		t.Fatal("another workspace must not move the workflow")
	}
}

func TestAProductionPinOfAnotherWorkspacesRunIsWithheld(t *testing.T) {
	repo := NewMockWorkflowRepository()
	wf := workflowPinnedFromRun()
	wf.Graph.FindNode("login").Pinned.Source = workflow.PinSourceOfRun("run-foreign")
	if err := repo.Create(wf); err != nil {
		t.Fatal(err)
	}
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(&workflow.WorkflowRun{ID: "run-foreign", WorkflowID: "wf-9", WorkspaceID: "ws-2", EntryID: "entry-1", EntryType: "whatsapp", State: workflow.NewRunState()})
	uc := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{Get: NewGetWorkflowUseCase(repo), Versions: NewWorkflowVersionsUseCase(repo), PinSources: NewRunPinSources(runs, seesEverything{})})

	got, err := uc.Detail(viewerScope(viewer.UserID), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if pin := got.Graph.FindNode("login").Pinned; !pin.Withheld || pin.Output != nil {
		t.Fatalf("pin = %+v", pin)
	}
}

func TestWithoutAPinSourceCheckEveryProductionPinIsWithheld(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(workflowPinnedFromRun()); err != nil {
		t.Fatal(err)
	}
	uc := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{Get: NewGetWorkflowUseCase(repo), Versions: NewWorkflowVersionsUseCase(repo)})
	got, err := uc.Detail(viewerScope(viewer.UserID), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if pin := got.Graph.FindNode("login").Pinned; !pin.Withheld || pin.Output != nil {
		t.Fatalf("pin = %+v", pin)
	}
}

func TestSavingTheDraftAsViewedKeepsTheWithheldOutputAndAnswersWithheld(t *testing.T) {
	hidden := inboxRule{departments: []string{supportDepartment}, viewOthers: true, conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment}}}
	uc, repo := pinWithholdingFixture(t, hidden)
	scope := viewerScope(viewer.UserID)
	viewed, err := uc.Detail(scope, "wf-1")
	if err != nil {
		t.Fatal(err)
	}

	saved, err := uc.Update(context.Background(), scope, "wf-1", &workflow.Workflow{Name: viewed.Name, Type: viewed.Type, Graph: viewed.Graph.Clone()}, viewed.Version)
	if err != nil {
		t.Fatalf("saving a draft with a withheld pin must work: %v", err)
	}
	if pin := saved.Graph.FindNode("login").Pinned; !pin.Withheld || pin.Output != nil {
		t.Fatalf("the save answer must not hand the hidden output back: %+v", pin)
	}
	stored, _ := repo.FindByID("wf-1")
	if pin := stored.Graph.FindNode("login").Pinned; pin == nil || pin.Withheld || pin.Output["cpf"] != "•••• 33" || pin.Edge != "falha" {
		t.Fatalf("the stored pin must keep its output: %+v", pin)
	}
}

func TestTheWorkflowListNeverCarriesProductionPinOutputs(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(workflowPinnedFromRun()); err != nil {
		t.Fatal(err)
	}
	page, err := NewListWorkflowsUseCase(repo).Execute(workflow.ListWorkflowsInput{WorkspaceID: "ws-1", Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if pin := page.Items[0].Graph.FindNode("login").Pinned; !pin.Withheld || pin.Output != nil {
		t.Fatalf("pin = %+v", pin)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Graph.FindNode("login").Pinned.Output == nil {
		t.Fatal("the list must not change the stored draft")
	}
}
