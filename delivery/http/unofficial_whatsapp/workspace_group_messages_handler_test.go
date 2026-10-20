package unofficial_whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
)

type feedScope struct{ allowed bool }

func (s feedScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, s.allowed
}

func readGroupMessages(h *WorkspaceGroupHandler, query string, workspaceID string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/groups/g-1/messages"+query, nil)
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "user"})
	if workspaceID != "" {
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, workspaceID)
	}
	w := httptest.NewRecorder()
	h.Messages(w, r.WithContext(ctx))
	return w
}

func TestGroupMessagesRefusesAnUnreadableCursor(t *testing.T) {
	h := NewWorkspaceGroupHandler(nil, feedScope{allowed: true})
	if w := readGroupMessages(h, "?before=yesterday", "ws-1"); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGroupMessagesRefusesWithoutScope(t *testing.T) {
	cases := []struct {
		name        string
		departments DepartmentScopeResolver
		workspaceID string
	}{
		{"no workspace", feedScope{allowed: true}, ""},
		{"scope denied", feedScope{}, "ws-1"},
		{"scope not wired", nil, "ws-1"},
	}
	for _, c := range cases {
		h := NewWorkspaceGroupHandler(nil, c.departments)
		if w := readGroupMessages(h, "", c.workspaceID); w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", c.name, w.Code)
		}
	}
}
