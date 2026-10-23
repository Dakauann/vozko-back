package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type departmentGuardedWorkflows struct {
	workflow.ScopedWorkflowsUseCase
	getErr   error
	assigned *workflow.Workflow
}

func (f *departmentGuardedWorkflows) Get(_ workflow.Scope, id string) (*workflow.Workflow, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &workflow.Workflow{ID: id, WorkspaceID: "ws1"}, nil
}

func (f *departmentGuardedWorkflows) AssignDepartment(_ context.Context, _ workflow.Scope, id string) (*workflow.Workflow, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.assigned = &workflow.Workflow{ID: id, DepartmentID: "dep-2"}
	return f.assigned, nil
}

type recordingStartRun struct {
	input  *workflow.StartRunInput
	err    error
	called bool
}

func (s *recordingStartRun) Execute(in workflow.StartRunInput) (*workflow.WorkflowRun, error) {
	s.called = true
	s.input = &in
	if s.err != nil {
		return nil, s.err
	}
	return &workflow.WorkflowRun{ID: "run-1", WorkflowID: in.WorkflowID}, nil
}

type recordingListRuns struct{ called bool }

func (s *recordingListRuns) Execute(shared.Person, workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	s.called = true
	return shared.NewPaginatedResult([]*workflow.WorkflowRun{}, shared.Pagination{Page: 1, PageSize: 20}, 0), nil
}

func TestStartingARunChecksTheWorkflowsDepartmentAndTheConversation(t *testing.T) {
	body := []byte(`{"entryId":"entry-1","entryType":"whatsapp"}`)
	cases := []struct {
		name     string
		getErr   error
		startErr error
		want     int
		started  bool
	}{
		{"visible conversation", nil, nil, http.StatusCreated, true},
		{"workflow of another department", workflow.ErrWorkflowAccessDenied, nil, http.StatusForbidden, false},
		{"hidden conversation", nil, conversation.ErrNotVisible, http.StatusNotFound, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := &recordingStartRun{err: tc.startErr}
			h := &WorkflowHandler{workflows: &departmentGuardedWorkflows{getErr: tc.getErr}, startRunUC: start}
			rec := httptest.NewRecorder()
			h.StartRun(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/runs", body, &auth.Claims{UserID: "u1"}))
			if rec.Code != tc.want || start.called != tc.started {
				t.Fatalf("status = %d started = %v body = %s", rec.Code, start.called, rec.Body.String())
			}
			if start.called && (start.input.Viewer.UserID != "u1" || start.input.WorkspaceID != "ws1" || start.input.StartedBy != workflow.StartedByManual("u1")) {
				t.Fatalf("input = %+v", start.input)
			}
		})
	}
}

func TestStartingARunCarriesTheMessageTheCustomerWouldHaveSent(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"entryId":"entry-1","entryType":"whatsapp","message":"Quero saber das turmas"}`, "Quero saber das turmas"},
		{`{"entryId":"entry-1","entryType":"whatsapp"}`, ""},
	} {
		start := &recordingStartRun{}
		h := &WorkflowHandler{workflows: &departmentGuardedWorkflows{}, startRunUC: start}
		rec := httptest.NewRecorder()
		h.StartRun(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/runs", []byte(tc.body), &auth.Claims{UserID: "u1"}))
		if rec.Code != http.StatusCreated || start.input == nil || start.input.Message != tc.want {
			t.Fatalf("status = %d input = %+v", rec.Code, start.input)
		}
	}
}

func TestStartingARunOnAWorkflowWithoutAMessageTriggerIsRefusedWithItsCode(t *testing.T) {
	start := &recordingStartRun{err: fmt.Errorf("start: %w", workflow.ErrWorkflowNeedsMessageTrigger)}
	h := &WorkflowHandler{workflows: &departmentGuardedWorkflows{}, startRunUC: start}
	rec := httptest.NewRecorder()
	h.StartRun(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/runs", []byte(`{"entryId":"entry-1","entryType":"whatsapp"}`), &auth.Claims{UserID: "u1"}))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), workflow.NeedsMessageTriggerCode) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestListingRunsOfAnotherDepartmentsWorkflowIsRefusedBeforeTheQuery(t *testing.T) {
	runs := &recordingListRuns{}
	h := &WorkflowHandler{workflows: &departmentGuardedWorkflows{getErr: workflow.ErrWorkflowAccessDenied}, listRunsUC: runs}
	rec := httptest.NewRecorder()
	h.ListRuns(rec, workflowRequestAs(http.MethodGet, "/workflows/w1/runs", nil, &auth.Claims{UserID: "u1"}))
	if rec.Code != http.StatusForbidden || runs.called {
		t.Fatalf("status = %d listed = %v; voice runs and runs without a conversation follow the workflow's department", rec.Code, runs.called)
	}
}

type recordingRunPages struct {
	window workflow.StepWindow
	asked  bool
}

func (s *recordingRunPages) ExecutePage(_ shared.Person, _, runID string, window workflow.StepWindow) (*workflow.RunDetail, error) {
	s.window, s.asked = window, true
	return &workflow.RunDetail{Run: &workflow.WorkflowRun{ID: runID}, Steps: &workflow.RunStepsPage{Offset: window.Offset, Limit: window.Limit, Total: 1200, HasMore: true}}, nil
}

func TestTheRunDetailReadsTheRequestedPageOfSteps(t *testing.T) {
	cases := []struct {
		query string
		want  workflow.StepWindow
		code  int
	}{
		{"", workflow.StepWindow{}, http.StatusOK},
		{"?stepsOffset=500&stepsLimit=250", workflow.StepWindow{Offset: 500, Limit: 250}, http.StatusOK},
		{"?stepsOffset=abc", workflow.StepWindow{}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			pages := &recordingRunPages{}
			r := mux.SetURLVars(workflowRequestAs(http.MethodGet, "/workflow-runs/run-1"+tc.query, nil, &auth.Claims{UserID: "u1"}), map[string]string{"runId": "run-1"})
			rec := httptest.NewRecorder()
			(&WorkflowHandler{getRunUC: pages}).GetRun(rec, r)
			if rec.Code != tc.code || (tc.code == http.StatusOK && pages.window != tc.want) || (tc.code != http.StatusOK && pages.asked) {
				t.Fatalf("status = %d window = %+v body = %s", rec.Code, pages.window, rec.Body.String())
			}
		})
	}
}

func TestAssigningADepartmentAnswersThroughTheScopedUseCase(t *testing.T) {
	workflows := &departmentGuardedWorkflows{}
	h := &WorkflowHandler{workflows: workflows}
	rec := httptest.NewRecorder()
	h.AssignDepartment(rec, workflowRequestAs(http.MethodPatch, "/workflows/w1/department", []byte(`{"departmentId":"dep-2"}`), &auth.Claims{UserID: "u1", Role: "admin"}))
	if rec.Code != http.StatusOK || workflows.assigned == nil {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
