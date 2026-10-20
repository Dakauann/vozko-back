package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type refusingWorkflows struct {
	workflow.ScopedWorkflowsUseCase
	scopes []workflow.Scope
	calls  []string
}

func (f *refusingWorkflows) refuse(op string, scope workflow.Scope) (*workflow.Workflow, error) {
	f.calls = append(f.calls, op)
	f.scopes = append(f.scopes, scope)
	return nil, workspace.ErrConsentGrantForbidden
}

func (f *refusingWorkflows) Create(_ context.Context, scope workflow.Scope, _ *workflow.Workflow) (*workflow.Workflow, error) {
	return f.refuse("create", scope)
}

func (f *refusingWorkflows) Update(_ context.Context, scope workflow.Scope, _ string, _ *workflow.Workflow, _ int) (*workflow.Workflow, error) {
	return f.refuse("update", scope)
}

func (f *refusingWorkflows) Activate(_ context.Context, scope workflow.Scope, _ string) (*workflow.Workflow, error) {
	return f.refuse("activate", scope)
}

func workflowRequestAs(method, path string, body []byte, claims *auth.Claims) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, claims)
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": "w1"})
}

func TestEveryWorkflowSaveAndActivationAnswersAConsentRefusalWith403(t *testing.T) {
	grantGraph := workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived},
			{ID: "grant", Type: workflow.NodeTypeActionConsent, Config: map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{{Source: "t", Target: "grant"}, {Source: "grant", Target: "end"}},
	}
	body, _ := json.Marshal(map[string]interface{}{"name": "optin", "type": "messages", "graph": grantGraph, "revision": 3})
	routes := []struct {
		op      string
		request *http.Request
		serve   func(h *WorkflowHandler, w http.ResponseWriter, r *http.Request)
	}{
		{"create", workflowRequestAs(http.MethodPost, "/workflows", body, &auth.Claims{UserID: "u1", Role: "admin"}), (*WorkflowHandler).Create},
		{"update", workflowRequestAs(http.MethodPut, "/workflows/w1", body, &auth.Claims{UserID: "u1", Role: "admin"}), (*WorkflowHandler).Update},
		{"create", workflowRequestAs(http.MethodPost, "/workflows/import", body, &auth.Claims{UserID: "u1", Role: "admin"}), (*WorkflowHandler).Import},
		{"activate", workflowRequestAs(http.MethodPost, "/workflows/w1/activate", nil, &auth.Claims{UserID: "u1", Role: "admin"}), (*WorkflowHandler).Activate},
	}
	for _, route := range routes {
		t.Run(route.request.URL.Path, func(t *testing.T) {
			workflows := &refusingWorkflows{}
			h := &WorkflowHandler{workflows: workflows}
			rec := httptest.NewRecorder()
			route.serve(h, rec, route.request)

			var resp struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusForbidden || resp.Code != "lead_consent_grant_forbidden" {
				t.Fatalf("status %d code %q body %s", rec.Code, resp.Code, rec.Body.String())
			}
			want := workspace.Requester{UserID: "u1", SystemAdmin: true}
			if len(workflows.calls) != 1 || workflows.calls[0] != route.op || workflows.scopes[0].Requester != want || workflows.scopes[0].WorkspaceID != "ws1" {
				t.Fatalf("calls %v scopes %+v", workflows.calls, workflows.scopes)
			}
		})
	}
}
