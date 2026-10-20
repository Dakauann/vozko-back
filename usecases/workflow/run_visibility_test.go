package workflow_usecase

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	salesDepartment   = "dept-sales"
	supportDepartment = "dept-support"
)

type inboxConversation struct {
	department string
	assignee   string
}

type inboxRule struct {
	departments   []string
	viewOthers    bool
	conversations map[string]inboxConversation
	err           error
}

func (r inboxRule) CanView(viewer shared.Person, workspaceID, entryID, _ string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	c, ok := r.conversations[entryID]
	if !ok || workspaceID == "" {
		return false, nil
	}
	if c.assignee != "" && c.assignee != viewer.UserID && !r.viewOthers {
		return false, nil
	}
	if r.departments != nil && !slices.Contains(r.departments, c.department) && c.assignee != viewer.UserID {
		return false, nil
	}
	return true, nil
}

func (r inboxRule) Scope(viewer shared.Person, _ string) (conversation.VisibilityScope, error) {
	if r.err != nil {
		return conversation.VisibilityScope{}, r.err
	}
	scope := conversation.VisibilityScope{}
	if r.departments != nil {
		scope.DepartmentIDs, scope.RestrictDepartments, scope.AssigneeOverrideUserID = r.departments, true, viewer.UserID
	}
	if !r.viewOthers {
		scope.AssignedUserID = viewer.UserID
	}
	return scope, nil
}

type seesEverything struct{}

func (seesEverything) CanView(shared.Person, string, string, string) (bool, error) { return true, nil }

func (seesEverything) Scope(shared.Person, string) (conversation.VisibilityScope, error) {
	return conversation.VisibilityScope{}, nil
}

var (
	viewer          = shared.Person{UserID: "user-1"}
	everyoneVisible = inboxRule{viewOthers: true, conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment}}}
	errScopeRead    = errors.New("department scope unreadable")
)

type visibilityCase struct {
	name     string
	rule     inboxRule
	visible  bool
	runEntry string
}

func runVisibilityCases() []visibilityCase {
	return []visibilityCase{
		{"visible", inboxRule{departments: []string{salesDepartment}, conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment}}}, true, "whatsapp"},
		{"hidden by department", inboxRule{departments: []string{supportDepartment}, viewOthers: true, conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment}}}, false, "whatsapp"},
		{"hidden by ownership without view_others", inboxRule{conversations: map[string]inboxConversation{"entry-1": {department: salesDepartment, assignee: "user-2"}}}, false, "whatsapp"},
		{"hidden when the lookup fails", inboxRule{err: errScopeRead, conversations: map[string]inboxConversation{"entry-1": {}}}, false, "whatsapp"},
		{"a call run has no conversation to hide", inboxRule{err: errScopeRead}, true, workflow.EntryTypeSIPCall},
	}
}

func conversationRun(entryType string) *MockWorkflowRunRepository {
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(&workflow.WorkflowRun{
		ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: entryType,
		Status: workflow.RunStatusWaiting, State: workflow.NewRunState(),
	})
	return runs
}

func TestARunIsOnlyReadByWhoCanSeeItsConversation(t *testing.T) {
	for _, tc := range runVisibilityCases() {
		t.Run(tc.name, func(t *testing.T) {
			detail, err := NewGetRunUseCase(conversationRun(tc.runEntry), NewMockWorkflowRunLogRepository(), tc.rule).Execute(viewer, "ws-1", "run-1")
			if tc.visible && (err != nil || detail == nil || detail.Run.ID != "run-1") {
				t.Fatalf("detail = %+v, err = %v", detail, err)
			}
			if !tc.visible && (!errors.Is(err, workflow.ErrRunNotFound) || detail != nil) {
				t.Fatalf("a hidden run must read as not found: detail = %+v, err = %v", detail, err)
			}
		})
	}
}

func TestARunIsOnlyCancelledByWhoCanSeeItsConversation(t *testing.T) {
	for _, tc := range runVisibilityCases() {
		t.Run(tc.name, func(t *testing.T) {
			runs := conversationRun(tc.runEntry)
			err := NewCancelRunUseCase(runs, runs, NewMockWorkflowRunLogRepository(), nil, tc.rule).Execute(viewer, "ws-1", "run-1")
			stored, _ := runs.FindByID("run-1")
			if tc.visible && (err != nil || stored.Status != workflow.RunStatusCancelled) {
				t.Fatalf("err = %v, status = %q", err, stored.Status)
			}
			if !tc.visible && (!errors.Is(err, workflow.ErrRunNotFound) || stored.Status != workflow.RunStatusWaiting) {
				t.Fatalf("a hidden run must not be cancelled: err = %v, status = %q", err, stored.Status)
			}
		})
	}
}

type scopeCase struct {
	name string
	rule inboxRule
	want conversation.VisibilityScope
}

func listScopeCases() []scopeCase {
	return []scopeCase{
		{"visible", everyoneVisible, conversation.VisibilityScope{}},
		{"hidden by department", inboxRule{departments: []string{supportDepartment}, viewOthers: true},
			conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{supportDepartment}, RestrictDepartments: true, AssigneeOverrideUserID: "user-1"}}},
		{"hidden by ownership without view_others", inboxRule{}, conversation.VisibilityScope{AssignedUserID: "user-1"}},
		{"hidden when the lookup fails", inboxRule{err: errScopeRead}, conversation.NothingVisible()},
	}
}

type scopedRunList struct {
	*MockWorkflowRunRepository
	inputs []workflow.ListRunsInput
}

func (s *scopedRunList) List(input workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	s.inputs = append(s.inputs, input)
	return s.MockWorkflowRunRepository.List(input)
}

func TestTheRunListHandsTheViewersInboxScopeToTheQuery(t *testing.T) {
	for _, tc := range listScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			runs := &scopedRunList{MockWorkflowRunRepository: conversationRun("whatsapp")}
			if _, err := NewListRunsUseCase(runs, tc.rule).Execute(viewer, workflow.ListRunsInput{WorkspaceID: "ws-1", WorkflowID: "wf-1"}); err != nil {
				t.Fatal(err)
			}
			if len(runs.inputs) != 1 || !reflect.DeepEqual(runs.inputs[0].Visibility, tc.want) {
				t.Fatalf("visibility = %+v, want %+v", runs.inputs, tc.want)
			}
		})
	}
}

func TestTheRunListIgnoresAVisibilityTheCallerTriedToPass(t *testing.T) {
	runs := &scopedRunList{MockWorkflowRunRepository: conversationRun("whatsapp")}
	input := workflow.ListRunsInput{WorkspaceID: "ws-1", WorkflowID: "wf-1", Visibility: conversation.VisibilityScope{}}
	if _, err := NewListRunsUseCase(runs, inboxRule{}).Execute(viewer, input); err != nil {
		t.Fatal(err)
	}
	if runs.inputs[0].Visibility.AssignedUserID != "user-1" {
		t.Fatalf("visibility = %+v", runs.inputs[0].Visibility)
	}
}

type scopedDeliveries struct {
	fakeDeliveryLog
	scopes []conversation.VisibilityScope
}

func (s *scopedDeliveries) ListByWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error) {
	s.scopes = append(s.scopes, visibility)
	return s.fakeDeliveryLog.ListByWorkflow(workspaceID, workflowID, visibility, pagination)
}

func TestWebhookDeliveriesHandTheViewersInboxScopeToTheQuery(t *testing.T) {
	for _, tc := range listScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			deliveries := &scopedDeliveries{}
			if _, err := NewListWebhookDeliveriesUseCase(deliveries, tc.rule).Execute(viewer, "ws-1", "wf-1", shared.Pagination{}); err != nil {
				t.Fatal(err)
			}
			if len(deliveries.scopes) != 1 || !reflect.DeepEqual(deliveries.scopes[0], tc.want) {
				t.Fatalf("visibility = %+v, want %+v", deliveries.scopes, tc.want)
			}
		})
	}
}

func TestRuntimeAlertListsHandTheViewersInboxScopeToTheQuery(t *testing.T) {
	for _, tc := range listScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newAlertHarness(t)
			h.uc.visibility = tc.rule
			if _, err := h.uc.ForWorkflow(viewer, "ws-1", "wf-1"); err != nil {
				t.Fatal(err)
			}
			if _, err := h.uc.OpenInWorkspace(viewer, workflow.Scope{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{}}, shared.Pagination{}); err != nil {
				t.Fatal(err)
			}
			if len(h.alerts.scopes) != 2 || !reflect.DeepEqual(h.alerts.scopes[0], tc.want) || !reflect.DeepEqual(h.alerts.scopes[1], tc.want) {
				t.Fatalf("visibility = %+v, want %+v", h.alerts.scopes, tc.want)
			}
		})
	}
}

func TestTheWorkspaceAlertListHandsTheDepartmentScopeToTheQueryFailingClosed(t *testing.T) {
	cases := []struct {
		name   string
		filter *wd.DepartmentFilter
		want   workflow.DepartmentRestriction
	}{
		{"no department scope read", nil, workflow.DepartmentRestriction{Restricted: true}},
		{"member of no department", &wd.DepartmentFilter{WorkspaceHasDepartments: true}, workflow.DepartmentRestriction{Restricted: true}},
		{"member of one department", &wd.DepartmentFilter{WorkspaceHasDepartments: true, DepartmentIDs: []string{"dep-1"}}, workflow.DepartmentRestriction{Restricted: true, DepartmentIDs: []string{"dep-1"}}},
		{"owner", &wd.DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true}, workflow.DepartmentRestriction{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAlertHarness(t)
			if _, err := h.uc.OpenInWorkspace(viewer, workflow.Scope{WorkspaceID: "ws-1", Departments: tc.filter}, shared.Pagination{}); err != nil {
				t.Fatal(err)
			}
			if len(h.alerts.restrictions) != 1 || !reflect.DeepEqual(h.alerts.restrictions[0], tc.want) {
				t.Fatalf("restriction = %+v, want %+v", h.alerts.restrictions, tc.want)
			}
		})
	}
}

type conversationRunsStub struct {
	items []*workflow.ConversationRun
	asked []string
}

func (s *conversationRunsStub) ListByEntry(workspaceID, entryID, entryType string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.ConversationRun], error) {
	s.asked = append(s.asked, workspaceID+"/"+entryType+"/"+entryID)
	return shared.NewPaginatedResult(s.items, shared.NormalizePagination(pagination), int64(len(s.items))), nil
}

type namedSteps map[workflow.NodeType]string

func (n namedSteps) StepName(nodeType workflow.NodeType) string { return n[nodeType] }

func TestTheConversationRunListNeedsTheConversationToBeVisible(t *testing.T) {
	started := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, tc := range runVisibilityCases() {
		if tc.runEntry == workflow.EntryTypeSIPCall {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			runs := &conversationRunsStub{items: []*workflow.ConversationRun{
				{ID: "run-2", Status: workflow.RunStatusWaiting, CurrentNodeID: "n4", CurrentNodeType: "action_wait_reply", StartedAt: started},
			}}
			uc := NewListConversationRunsUseCase(runs, tc.rule, namedSteps{"action_wait_reply": "Aguardar resposta"})
			page, err := uc.Execute(viewer, "ws-1", "whatsapp", "entry-1", shared.Pagination{})
			if !tc.visible {
				if !errors.Is(err, conversation.ErrNotVisible) || page != nil || len(runs.asked) != 0 {
					t.Fatalf("a hidden conversation must not list runs: page = %+v, err = %v, asked = %v", page, err, runs.asked)
				}
				return
			}
			if err != nil || len(page.Items) != 1 || page.Items[0].CurrentNodeName != "Aguardar resposta" {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
			if runs.asked[0] != "ws-1/whatsapp/entry-1" {
				t.Fatalf("asked = %v", runs.asked)
			}
		})
	}
}

func TestTheConversationRunListRefusesAMissingConversation(t *testing.T) {
	uc := NewListConversationRunsUseCase(&conversationRunsStub{}, everyoneVisible, namedSteps{})
	for _, args := range [][3]string{{"", "whatsapp", "entry-1"}, {"ws-1", "", "entry-1"}, {"ws-1", "whatsapp", ""}} {
		if _, err := uc.Execute(viewer, args[0], args[1], args[2], shared.Pagination{}); !errors.Is(err, conversation.ErrNotVisible) {
			t.Fatalf("%v: err = %v", args, err)
		}
	}
}
