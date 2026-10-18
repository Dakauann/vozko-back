package unofficial_whatsapp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type recordingCreate struct{ called bool }

func (c *recordingCreate) Execute(_ context.Context, in *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	c.called = true
	return in, nil
}

type groupsAccess struct{ allowed bool }

func (a groupsAccess) Execute(_, _ string, resource workspace_domain.Resource, action workspace_domain.Action) error {
	if resource == workspace_domain.ResourceUnofficialWhatsAppGroups && action == workspace_domain.ActionRead && a.allowed {
		return nil
	}
	return workspace_domain.ErrResourceAccessDenied
}

func createGroupCampaign(h *CampaignHandler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/campaigns", bytes.NewBufferString(
		`{"name":"demo","instanceId":"inst-1","audience":"groups","message":{"kind":"text","bodies":["oi"]},"targets":[{"groupJid":"120363000000000001@g.us"}]}`))
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "user"})
	w := httptest.NewRecorder()
	h.Create(w, r.WithContext(ctx))
	return w
}

func TestAGroupCampaignNeedsPermissionToSeeTheGroups(t *testing.T) {
	cases := []struct {
		name        string
		permissions workspace_domain.CheckAccessUseCase
		want        int
	}{
		{"allowed", groupsAccess{allowed: true}, http.StatusCreated},
		{"denied", groupsAccess{}, http.StatusForbidden},
		{"not wired", nil, http.StatusForbidden},
	}
	for _, tc := range cases {
		create := &recordingCreate{}
		h := NewCampaignHandler(CampaignHandlerDeps{Create: create, Departments: openScope{}, Permissions: tc.permissions})
		w := createGroupCampaign(h)
		if w.Code != tc.want || create.called != (tc.want == http.StatusCreated) {
			t.Errorf("%s: status %d created %v, want %d", tc.name, w.Code, create.called, tc.want)
		}
	}
}
