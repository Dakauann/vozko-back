package customfield

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	customfielddomain "vozko/domain/customfield"
	"vozko/infra/http/middleware"
	customfield_usecase "vozko/usecases/customfield"
)

type listOnlyRepository struct {
	customfielddomain.Store
	defs []*customfielddomain.Definition
}

func (r listOnlyRepository) ListByObject(string, customfielddomain.ObjectType) ([]*customfielddomain.Definition, error) {
	return r.defs, nil
}

type permissionStub struct {
	allowed map[string]bool
	asked   []string
}

func (p *permissionStub) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	p.asked = append(p.asked, resource+":"+action)
	return p.allowed[resource+":"+action]
}

func listRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/custom-fields?objectType=lead", nil)
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return req.WithContext(ctx)
}

func readableByKey(t *testing.T, rec *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	var body []struct {
		Key      string `json:"key"`
		Readable *bool  `json:"readable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	out := map[string]bool{}
	for _, item := range body {
		if item.Readable == nil {
			t.Fatalf("definition %s has no readable verdict", item.Key)
		}
		out[item.Key] = *item.Readable
	}
	return out
}

func TestListTellsEachViewerWhichDefinitionsTheyMayRead(t *testing.T) {
	defs := []*customfielddomain.Definition{
		{ID: "a", Key: "bairro", ObjectType: customfielddomain.ObjectLead},
		{ID: "b", Key: "classificacao", ObjectType: customfielddomain.ObjectLead, Sensitive: true},
	}
	cases := []struct {
		name        string
		permissions Permissions
		want        map[string]bool
	}{
		{"without the sensitive permission", &permissionStub{allowed: map[string]bool{"leads:read": true}}, map[string]bool{"bairro": true, "classificacao": false}},
		{"with the sensitive permission", &permissionStub{allowed: map[string]bool{"leads:read": true, "leads:read_sensitive": true}}, map[string]bool{"bairro": true, "classificacao": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewCustomFieldHandler(customfield_usecase.NewService(listOnlyRepository{defs: defs}, tc.permissions), tc.permissions)
			rec := httptest.NewRecorder()
			h.List(rec, listRequest())
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
			}
			got := readableByKey(t, rec)
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s readable = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}

func TestListRefusesWithoutThePermissionOfTheObject(t *testing.T) {
	defs := []*customfielddomain.Definition{{ID: "a", Key: "bairro", ObjectType: customfielddomain.ObjectLead}}
	for name, perms := range map[string]Permissions{
		"a conversations reader": &permissionStub{allowed: map[string]bool{"conversations:read": true}},
		"no permission checker":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			h := NewCustomFieldHandler(customfield_usecase.NewService(listOnlyRepository{defs: defs}, perms), perms)
			rec := httptest.NewRecorder()
			h.List(rec, listRequest())
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d (%s), want 403", rec.Code, rec.Body.String())
			}
		})
	}
}
