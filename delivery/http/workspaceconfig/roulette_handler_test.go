package workspaceconfig

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/inbox_assignment"
	workspaceconfigdomain "vozko/domain/workspace_config"
	"vozko/infra/http/middleware"
)

type stubOwnerUpdate struct {
	err   error
	input workspaceconfigdomain.UpdateWorkspaceConfigOwnerInput
}

func (s *stubOwnerUpdate) Execute(_ context.Context, _, _, _ string, input workspaceconfigdomain.UpdateWorkspaceConfigOwnerInput) (*workspaceconfigdomain.WorkspaceConfig, error) {
	s.input = input
	if s.err != nil {
		return nil, s.err
	}
	return &workspaceconfigdomain.WorkspaceConfig{WorkspaceID: "ws-1"}, nil
}

func putConfig(update *stubOwnerUpdate, body string) *httptest.ResponseRecorder {
	r := mux.NewRouter()
	RegisterProtectedRoutes(r, NewWorkspaceConfigHandler(nil, nil, update))
	req := httptest.NewRequest(http.MethodPut, "/workspaces/ws-1/config", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, owner))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUpdate_PassesTheRouletteStrategyThrough(t *testing.T) {
	update := &stubOwnerUpdate{}
	rec := putConfig(update, `{"rouletteStrategy":"priority","rouletteMaxOpenPerMember":4,"rouletteRecipients":[{"userId":"ana","tier":1,"weight":2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	in := update.input
	if in.RouletteStrategy == nil || *in.RouletteStrategy != "priority" || in.RouletteMaxOpenPerMember == nil || *in.RouletteMaxOpenPerMember != 4 {
		t.Fatalf("got %+v", in)
	}
	if in.RouletteRecipients == nil || len(*in.RouletteRecipients) != 1 || (*in.RouletteRecipients)[0].Weight != 2 {
		t.Fatalf("got %+v", in.RouletteRecipients)
	}
}

func TestUpdate_RoulettePolicyErrorsAreBadRequests(t *testing.T) {
	for _, policy := range []error{inbox_assignment.ErrRecipientsRequired, inbox_assignment.ErrRecipientNotMember, inbox_assignment.ErrInvalidStrategy, inbox_assignment.ErrInvalidCapacity} {
		rec := putConfig(&stubOwnerUpdate{err: fmt.Errorf("wrapped: %w", policy)}, `{"rouletteStrategy":"priority"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%v: status %d", policy, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), policy.Error()) {
			t.Fatalf("%v: body %s", policy, rec.Body)
		}
	}
}
