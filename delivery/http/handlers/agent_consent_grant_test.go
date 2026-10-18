package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/agent"
	"vozko/domain/auth"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type refusingAgentCreate struct{ got agent.CreateAgentInput }

func (f *refusingAgentCreate) Execute(_ context.Context, in agent.CreateAgentInput) (*agent.Agent, error) {
	f.got = in
	return nil, workspace.ErrConsentGrantForbidden
}

type refusingAgentUpdate struct{ got agent.UpdateAgentInput }

func (f *refusingAgentUpdate) Execute(_ context.Context, _ string, in agent.UpdateAgentInput) (*agent.Agent, error) {
	f.got = in
	return nil, workspace.ErrConsentGrantForbidden
}

type ownAgent struct{}

func (ownAgent) Execute(id string) (*agent.Agent, error) {
	return &agent.Agent{ID: id, WorkspaceID: "ws1"}, nil
}

func agentRequestAs(method string, body []byte) *http.Request {
	r := httptest.NewRequest(method, "/agents/ag-1", bytes.NewReader(body))
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1", Role: "user"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": "ag-1"})
}

func agentBody(t *testing.T) []byte {
	t.Helper()
	isActive := true
	body, err := json.Marshal(map[string]interface{}{
		"name": "Ana", "messagingPrompt": "p", "messagingModel": "openai/gpt-4o", "provider": "platform", "isActive": isActive,
		"internalTools": []map[string]interface{}{{"name": "record_consent", "config": map[string]interface{}{"purpose": "ofertas"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertConsentRefusal(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var resp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden || resp.Code != "lead_consent_grant_forbidden" {
		t.Fatalf("status %d code %q body %s", rec.Code, resp.Code, rec.Body.String())
	}
}

func TestAgentCreateAndUpdatePassTheCallerAndAnswerAConsentRefusalWith403(t *testing.T) {
	create, update := &refusingAgentCreate{}, &refusingAgentUpdate{}
	h := &AgentHandler{createUseCase: create, updateUseCase: update, getUseCase: ownAgent{}}
	want := workspace.Requester{UserID: "u1"}

	rec := httptest.NewRecorder()
	h.Create(rec, agentRequestAs(http.MethodPost, agentBody(t)))
	assertConsentRefusal(t, rec)
	if create.got.RequestedBy != want {
		t.Fatalf("create requester = %+v", create.got.RequestedBy)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, agentRequestAs(http.MethodPut, agentBody(t)))
	assertConsentRefusal(t, rec)
	if update.got.RequestedBy != want {
		t.Fatalf("update requester = %+v", update.got.RequestedBy)
	}
}
