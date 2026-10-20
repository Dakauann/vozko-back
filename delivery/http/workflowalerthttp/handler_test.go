package workflowalerthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

var seenAt = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type stubAlerts struct {
	viewer                  shared.Person
	workspaceID, workflowID string
	scope                   workflow.Scope
	page                    shared.Pagination
	err                     error
}

func (s *stubAlerts) ForWorkflow(viewer shared.Person, workspaceID, workflowID string) ([]workflow.RuntimeAlert, error) {
	s.viewer, s.workspaceID, s.workflowID = viewer, workspaceID, workflowID
	if s.err != nil {
		return nil, s.err
	}
	return []workflow.RuntimeAlert{
		workflow.RuntimeAlert{
			ID: "a-1", Key: workflow.RuntimeAlertKey{WorkspaceID: workspaceID, WorkflowID: workflowID, NodeID: "n55", Kind: workflow.AlertNodeFailing},
			Count: 3, FirstSeen: seenAt, LastSeen: seenAt, Sample: "run-7", Status: workflow.RuntimeAlertOpen,
		}.Described(),
	}, nil
}

func (s *stubAlerts) OpenInWorkspace(viewer shared.Person, scope workflow.Scope, p shared.Pagination) (*shared.PaginatedResult[*workflow.RuntimeAlert], error) {
	workspaceID := scope.WorkspaceID
	s.viewer, s.workspaceID, s.scope, s.page = viewer, workspaceID, scope, p
	if s.err != nil {
		return nil, s.err
	}
	alert := workflow.RuntimeAlert{
		ID: "a-2", Key: workflow.RuntimeAlertKey{WorkspaceID: workspaceID, Subject: "instagram", Kind: workflow.AlertInboundLookupFailed},
		Count: 1, FirstSeen: seenAt, LastSeen: seenAt, Sample: "e-1", Status: workflow.RuntimeAlertOpen,
	}.Described()
	return shared.NewPaginatedResult([]*workflow.RuntimeAlert{&alert}, p, 1), nil
}

type stubAccess struct {
	err   error
	scope workflow.Scope
}

func (s *stubAccess) Get(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	s.scope = scope
	if s.err != nil {
		return nil, s.err
	}
	return &workflow.Workflow{ID: id, WorkspaceID: scope.WorkspaceID}, nil
}

func request(path string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = mux.SetURLVars(r, vars)
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"}))
}

func TestAlertsAreListedForTheViewerSoConversationAlertsFollowTheInboxRule(t *testing.T) {
	alerts := &stubAlerts{}
	NewHandler(alerts, &stubAccess{}).ListForWorkflow(httptest.NewRecorder(), request("/workflows/wf-1/alerts", map[string]string{"id": "wf-1"}))
	if alerts.viewer.UserID != "user-1" {
		t.Fatalf("workflow list viewer = %+v", alerts.viewer)
	}
	alerts = &stubAlerts{}
	NewHandler(alerts, &stubAccess{}).ListOpen(httptest.NewRecorder(), request("/workflow-alerts", nil))
	if alerts.viewer.UserID != "user-1" {
		t.Fatalf("workspace list viewer = %+v", alerts.viewer)
	}
}

func TestAlertsNeedAnAuthenticatedViewer(t *testing.T) {
	for name, list := range map[string]func(*Handler) http.HandlerFunc{
		"workflow":  func(h *Handler) http.HandlerFunc { return h.ListForWorkflow },
		"workspace": func(h *Handler) http.HandlerFunc { return h.ListOpen },
	} {
		t.Run(name, func(t *testing.T) {
			alerts := &stubAlerts{}
			r := httptest.NewRequest(http.MethodGet, "/workflow-alerts", nil)
			r = mux.SetURLVars(r, map[string]string{"id": "wf-1"})
			r = r.WithContext(context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1"))
			rec := httptest.NewRecorder()
			list(NewHandler(alerts, &stubAccess{}))(rec, r)
			if rec.Code != http.StatusUnauthorized || alerts.workspaceID != "" {
				t.Fatalf("status = %d, listed for %q", rec.Code, alerts.workspaceID)
			}
		})
	}
}

func TestAWorkflowsAlertsCarryTheirFixActionAndTheRunToOpen(t *testing.T) {
	alerts, access := &stubAlerts{}, &stubAccess{}
	rec := httptest.NewRecorder()
	NewHandler(alerts, access).ListForWorkflow(rec, request("/workflows/wf-1/alerts", map[string]string{"id": "wf-1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if alerts.workspaceID != "ws-1" || alerts.workflowID != "wf-1" || access.scope.WorkspaceID != "ws-1" {
		t.Fatalf("alerts = %+v, access = %+v", alerts, access)
	}
	var body WorkflowAlertsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Items) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	got := body.Items[0]
	if got.Kind != "node_failing" || got.Severity != "high" || got.FixAction != "open_run" || got.RunID != "run-7" || got.NodeID != "n55" || got.Status != "open" || got.Count != 3 {
		t.Fatalf("alert = %+v", got)
	}
}

func TestAWorkflowOutsideTheCallersReachHasNoAlerts(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"other workspace":  {workflow.ErrWorkflowNotFound, http.StatusNotFound},
		"other department": {workflow.ErrWorkflowAccessDenied, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			alerts := &stubAlerts{}
			rec := httptest.NewRecorder()
			NewHandler(alerts, &stubAccess{err: tc.err}).ListForWorkflow(rec, request("/workflows/wf-1/alerts", map[string]string{"id": "wf-1"}))
			if rec.Code != tc.code || alerts.workflowID != "" {
				t.Fatalf("status = %d, listed = %q", rec.Code, alerts.workflowID)
			}
		})
	}
}

func TestTheWorkspaceListIsPaginatedAndNamesTheConversation(t *testing.T) {
	alerts := &stubAlerts{}
	rec := httptest.NewRecorder()
	NewHandler(alerts, &stubAccess{}).ListOpen(rec, request("/workflow-alerts?page=2&pageSize=10", nil))

	if rec.Code != http.StatusOK || alerts.page.Page != 2 || alerts.page.PageSize != 10 {
		t.Fatalf("status = %d page = %+v", rec.Code, alerts.page)
	}
	var body struct {
		Data []AlertResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Data) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if got := body.Data[0]; got.FixAction != "open_conversation" || got.EntryID != "e-1" || got.EntryType != "instagram" || got.WorkflowID != "" {
		t.Fatalf("alert = %+v", got)
	}
}

func TestTheWorkspaceListHandsTheDepartmentScopeToTheUseCase(t *testing.T) {
	alerts := &stubAlerts{}
	filter := &wd.DepartmentFilter{WorkspaceHasDepartments: true}
	r := request("/workflow-alerts", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.DepartmentFilterContextKey, filter))
	rec := httptest.NewRecorder()
	NewHandler(alerts, &stubAccess{}).ListOpen(rec, r)
	if rec.Code != http.StatusOK || alerts.scope.WorkspaceID != "ws-1" || alerts.scope.Departments != filter {
		t.Fatalf("status = %d, scope = %+v; the use case decides what an empty department list shows", rec.Code, alerts.scope)
	}
}

func TestAnUnreadableAlertListIsAServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(&stubAlerts{err: errors.New("db down")}, &stubAccess{}).ListOpen(rec, request("/workflow-alerts", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}
