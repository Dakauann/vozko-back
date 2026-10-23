package emailhygienehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
	hyuc "vozko/usecases/emailhygiene"
)

type statusFake struct {
	got    string
	status hyuc.AddressStatus
	err    error
}

func (f *statusFake) AddressStatus(_ context.Context, ws, raw string) (hyuc.AddressStatus, error) {
	f.got = ws + "|" + raw
	return f.status, f.err
}

func request(address string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/email/address-status?address="+address, nil)
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1"})
	return r.WithContext(context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1"))
}

func TestAddressStatusExplainsASharedHardBounceWithoutNamingTheOtherWorkspace(t *testing.T) {
	fake := &statusFake{status: hyuc.AddressStatus{Address: emailaddress.Address("gone@loja.com.br"), Verdict: h.VerdictBadAddress,
		Standing: h.Standing{BadAddress: true, Domain: h.DomainAccepts}}}
	rec := httptest.NewRecorder()
	NewHandler(fake).AddressStatus(rec, request("gone%40loja.com.br"))
	var body AddressStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if fake.got != "ws1|gone@loja.com.br" || body.Verdict != string(h.VerdictBadAddress) || !body.BlocksCampaigns || body.Suppression == nil ||
		body.Suppression.Scope != "shared" || body.Suppression.Reason != "hard_bounce" || !body.NeverMailed {
		t.Fatalf("body = %+v", body)
	}
}

func TestAddressStatusShowsTheWorkspaceSuppressionFirst(t *testing.T) {
	fake := &statusFake{status: hyuc.AddressStatus{Address: "ana@loja.com.br", Verdict: h.VerdictSuppressed,
		Standing: h.Standing{Suppression: es.SuppressSoftBounce, BadAddress: true, Domain: h.DomainAccepts}}}
	rec := httptest.NewRecorder()
	NewHandler(fake).AddressStatus(rec, request("ana%40loja.com.br"))
	var body AddressStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Suppression == nil || body.Suppression.Scope != "workspace" || body.Suppression.Reason != "soft_bounce" {
		t.Fatalf("body = %+v", body)
	}
}

func TestAddressStatusRefusalsAreCoded(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{err: hyuc.ErrAddressInvalid, status: http.StatusUnprocessableEntity, code: "email_address_invalid"},
		{err: errors.New("db down"), status: http.StatusServiceUnavailable, code: "email_address_status_unavailable"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		NewHandler(&statusFake{err: tc.err}).AddressStatus(rec, request("x"))
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.status || body.Code != tc.code {
			t.Fatalf("%v: status %d code %q", tc.err, rec.Code, body.Code)
		}
	}
}

func TestTheRouteNeedsEmailReadAccess(t *testing.T) {
	router := mux.NewRouter()
	var guarded string
	RegisterProtectedRoutes(router, NewHandler(&statusFake{}), func(res workspace_domain.Resource, action workspace_domain.Action, f http.HandlerFunc) http.HandlerFunc {
		guarded = string(res) + ":" + string(action)
		return f
	})
	if guarded != string(workspace_domain.ResourceEmailMarketing)+":"+string(workspace_domain.ActionRead) {
		t.Fatalf("guard = %q", guarded)
	}
}

func TestAddressStatusCodesMatchTheGolden(t *testing.T) {
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), RefusalCodes(), "update testdata/error_codes.golden and translate emailMarketing.errors.<code> in the pt, en, es and de catalogs of vozko-front")
}
