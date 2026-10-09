package customfield

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	customfielddomain "vozko/domain/customfield"
	"vozko/infra/http/middleware"
	customfield_usecase "vozko/usecases/customfield"
)

type definitionRepository struct {
	customfielddomain.Store
	defs    map[string]*customfielddomain.Definition
	updated int
}

func (r *definitionRepository) GetByID(workspaceID, id string) (*customfielddomain.Definition, error) {
	d, ok := r.defs[id]
	if !ok || d.WorkspaceID != workspaceID {
		return nil, customfielddomain.ErrNotFound
	}
	copied := *d
	return &copied, nil
}

func (r *definitionRepository) ListByObject(string, customfielddomain.ObjectType) ([]*customfielddomain.Definition, error) {
	return nil, nil
}

func (r *definitionRepository) Update(*customfielddomain.Definition) error {
	r.updated++
	return nil
}

func TestAConversationsUpdaterCannotPatchALeadDefinitionOverHTTP(t *testing.T) {
	repo := &definitionRepository{defs: map[string]*customfielddomain.Definition{
		"lead-1": {ID: "lead-1", WorkspaceID: "ws-1", ObjectType: customfielddomain.ObjectLead, Key: "cor", Label: "Cor", Type: customfielddomain.TypeText},
	}}
	perms := &permissionStub{allowed: map[string]bool{"conversations:read": true, "conversations:update": true}}
	h := NewCustomFieldHandler(customfield_usecase.NewService(repo, perms), perms)

	router := mux.NewRouter()
	RegisterRoutes(router, h)
	req := httptest.NewRequest(http.MethodPatch, "/custom-fields/lead-1", strings.NewReader(`{"label":"Nova"}`))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d (%s), want 403", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "custom_field_forbidden" {
		t.Fatalf("code = %q (%v), want custom_field_forbidden", body.Code, err)
	}
	if repo.updated != 0 {
		t.Fatal("a refused update must not write")
	}
}
