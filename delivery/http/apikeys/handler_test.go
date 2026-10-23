package apikeys

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response/responsetest"
	"vozko/domain/apikey"
	workspace_domain "vozko/domain/workspace"
	apikey_usecase "vozko/usecases/apikey"
)

type keysServiceFake struct {
	created apikey_usecase.CreateInput
	err     error
	revoked string
}

func (f *keysServiceFake) Create(_ context.Context, in apikey_usecase.CreateInput) (apikey_usecase.Created, error) {
	f.created = in
	return apikey_usecase.Created{Key: apikey.Key{ID: "k-1", LookupID: "abcdefghijkl", Scopes: []apikey.Scope{apikey.ScopeEmailSend}}, Plain: "wsk_abcdefghijkl_secret"}, f.err
}

func (f *keysServiceFake) List(context.Context, string) ([]apikey.Key, error) {
	return []apikey.Key{{ID: "k-1", LookupID: "abcdefghijkl", SecretHash: "hash"}}, f.err
}

func (f *keysServiceFake) Revoke(_ context.Context, ws, id, _ string) (apikey.Key, error) {
	f.revoked = ws + "/" + id
	return apikey.Key{ID: id}, f.err
}

func keysRouter(svc *keysServiceFake) (*mux.Router, *[]string) {
	router := mux.NewRouter()
	var guards []string
	RegisterProtectedRoutes(router, NewHandler(svc), func(res workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		guards = append(guards, string(res)+":"+string(action))
		return next
	})
	return router, &guards
}

func call(router *mux.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Workspace-ID", "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestKeyManagementRoutesAreGuardedByTheAPIKeysPermission(t *testing.T) {
	_, guards := keysRouter(&keysServiceFake{})
	want := "api_keys:read api_keys:create api_keys:delete"
	if strings.Join(*guards, " ") != want {
		t.Fatalf("guards = %v", *guards)
	}
}

func TestCreateShowsTheKeyOnceAndListNeverDoes(t *testing.T) {
	svc := &keysServiceFake{}
	router, _ := keysRouter(svc)
	rec := call(router, http.MethodPost, "/api-keys", `{"name":"Produção","scopes":["email:send"]}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"key":"wsk_abcdefghijkl_secret"`) || svc.created.WorkspaceID != "ws-1" {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	rec = call(router, http.MethodGet, "/api-keys", "")
	var list KeyListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if strings.Contains(rec.Body.String(), "hash") || list.Keys[0].Prefix != "wsk_abcdefghijkl" || len(list.Products) == 0 || list.Products[0].Scopes[0].Scope != "email:send" {
		t.Fatalf("list = %s", rec.Body.String())
	}
}

func TestManagementRefusalsAreCoded(t *testing.T) {
	cases := map[error]string{
		apikey.ErrNameInvalid:              CodeNameInvalid,
		apikey.ErrScopesInvalid:            CodeScopesInvalid,
		apikey.ErrLimitReached:             CodeLimitReached,
		apikey_usecase.ErrCacheUnavailable: CodeCacheUnavailable,
	}
	for err, code := range cases {
		router, _ := keysRouter(&keysServiceFake{err: err})
		if rec := call(router, http.MethodPost, "/api-keys", `{"name":"x","scopes":["email:send"]}`); responsetest.CodeOf(t, rec) != code {
			t.Fatalf("%v = %s", err, rec.Body.String())
		}
	}
	svc := &keysServiceFake{}
	router, _ := keysRouter(svc)
	if rec := call(router, http.MethodDelete, "/api-keys/nope", ""); rec.Code != http.StatusNotFound || svc.revoked != "" {
		t.Fatalf("bad id = %d", rec.Code)
	}
	if rec := call(router, http.MethodDelete, "/api-keys/0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b", ""); rec.Code != http.StatusOK || svc.revoked != "ws-1/0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b" {
		t.Fatalf("revoke = %d %s", rec.Code, svc.revoked)
	}
}
