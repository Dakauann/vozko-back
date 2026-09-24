package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/infra/http/middleware"
	ia_usecase "vozko/usecases/inbox_assignment"
)

type stubAutomationToggle struct {
	got ia_usecase.OperatorAutomationInput
	res ia_usecase.OperatorAutomationResult
	err error
}

func (s *stubAutomationToggle) SetAutomation(_ context.Context, in ia_usecase.OperatorAutomationInput) (ia_usecase.OperatorAutomationResult, error) {
	s.got = in
	return s.res, s.err
}

func automationRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPatch, "/conversations/telegram/entry-1/automation", strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"entryType": "telegram", "entryId": "entry-1"})
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "bob"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(ctx)
}

func TestSetAutomation_PassesTheCallerAndReturnsTheOwner(t *testing.T) {
	toggle := &stubAutomationToggle{res: ia_usecase.OperatorAutomationResult{Owner: "ai:agent-1"}}
	h := &ConversationHandler{}
	h.SetAutomationService(toggle)

	w := httptest.NewRecorder()
	h.SetAutomation(w, automationRequest(`{"automationEnabled":true}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if toggle.got.ActorUserID != "bob" || toggle.got.WorkspaceID != "ws-1" || toggle.got.EntryID != "entry-1" || toggle.got.EntryType != "telegram" {
		t.Fatalf("input = %+v, want bob on ws-1/entry-1/telegram", toggle.got)
	}
	if toggle.got.Enabled == nil || !*toggle.got.Enabled {
		t.Fatalf("enabled = %v, want true", toggle.got.Enabled)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["assigned_user_id"] != "ai:agent-1" {
		t.Fatalf("assigned_user_id = %v, want ai:agent-1 (the web removes the row with it)", body["assigned_user_id"])
	}
}

func TestSetAutomation_ACallerWithoutAccessIsForbidden(t *testing.T) {
	h := &ConversationHandler{}
	h.SetAutomationService(&stubAutomationToggle{err: ia_usecase.ErrAutomationForbidden})

	w := httptest.NewRecorder()
	h.SetAutomation(w, automationRequest(`{"automationEnabled":false}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
