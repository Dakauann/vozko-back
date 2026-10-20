package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	mluc "vozko/usecases/mercadolivre"
	"vozko/usecases/shared/automationrefs"
	"vozko/usecases/shared/oauthstate"
)

type fakeConnector struct {
	start    *mluc.StartConnectOutput
	complete *mluc.CompleteConnectOutput
	err      error
	got      mluc.CompleteConnectInput
	started  mluc.StartConnectInput
}

func (f *fakeConnector) Start(_ context.Context, in mluc.StartConnectInput) (*mluc.StartConnectOutput, error) {
	f.started = in
	return f.start, f.err
}

func (f *fakeConnector) Complete(_ context.Context, in mluc.CompleteConnectInput) (*mluc.CompleteConnectOutput, error) {
	f.got = in
	return f.complete, f.err
}

type fakeAccounts struct {
	account    *mldomain.Account
	err        error
	update     mluc.UpdateAccountInput
	disconnect *mluc.DisconnectOutput
}

func (f *fakeAccounts) List(context.Context, mldomain.ListAccountsInput) (*shared.PaginatedResult[*mldomain.Account], error) {
	return shared.NewPaginatedResult([]*mldomain.Account{f.account}, shared.Pagination{Page: 1, PageSize: 20}, 1), f.err
}

func (f *fakeAccounts) Get(context.Context, string, string) (*mldomain.Account, error) {
	return f.account, f.err
}

func (f *fakeAccounts) Update(_ context.Context, in mluc.UpdateAccountInput) (*mldomain.Account, error) {
	f.update = in
	return f.account, f.err
}

func (f *fakeAccounts) Disconnect(context.Context, string, string) (*mluc.DisconnectOutput, error) {
	return f.disconnect, f.err
}

type fakeHealth struct {
	account *mldomain.Account
	err     error
}

func (f *fakeHealth) CheckInWorkspace(context.Context, string, string) (*mldomain.Account, error) {
	return f.account, f.err
}

func sampleAccount() *mldomain.Account {
	expires := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	checked := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	return &mldomain.Account{
		ID: "acct-1", WorkspaceID: "ws-1", MLUserID: "415458330", SiteID: "MLB", Nickname: "LOJA",
		AccessToken: "APP_USR-secret", RefreshToken: "TG-secret", AccessExpiresAt: &expires,
		GrantedScopes: []string{"offline_access", "read", "write"}, Status: mldomain.StatusConnected,
		CanReadMessages: true, CanSendMessages: true, CanReadQuestions: true, CanAnswerQuestions: true,
		LastHealthCheckAt: &checked, AgentID: strPtr("agent-1"), EnableAgentResponses: true,
	}
}

func strPtr(s string) *string { return &s }

func newTestHandler() (*Handler, *fakeConnector, *fakeAccounts, *fakeHealth) {
	c := &fakeConnector{}
	a := &fakeAccounts{account: sampleAccount()}
	h := &fakeHealth{account: sampleAccount()}
	return NewHandler(HandlerDeps{Connect: c, Accounts: a, Health: h, FrontendBaseURL: "https://app.example.com/"}), c, a, h
}

func withWorkspace(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1"))
}

func TestTheAccountResponseShape(t *testing.T) {
	h, _, _, _ := newTestHandler()
	rec := httptest.NewRecorder()
	req := mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodGet, "/mercadolivre/accounts/acct-1", nil)), map[string]string{"id": "acct-1"})
	h.GetAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret") {
		t.Fatalf("tokens leaked: %s", body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{
		"accessExpiresAt", "agentId", "automationDisclosure", "canAnswerQuestions", "canReadMessages", "canReadQuestions", "canSendMessages",
		"createdAt", "enableAgentResponses", "enableWorkflow", "grantedScopes", "id", "lastHealthCheckAt", "mlUserId", "needsReconnect",
		"nickname", "siteId", "status", "updatedAt", "workspaceId",
	}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v", keys)
	}
	if got["needsReconnect"] != false || got["canSendMessages"] != true {
		t.Fatalf("body = %s", body)
	}
}

func TestUpdatePassesOnlyTheFieldsSent(t *testing.T) {
	h, _, accounts, _ := newTestHandler()
	rec := httptest.NewRecorder()
	req := mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodPatch, "/mercadolivre/accounts/acct-1",
		strings.NewReader(`{"agentId":"agent-2","enableWorkflow":false}`))), map[string]string{"id": "acct-1"})
	h.UpdateAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	in := accounts.update
	if in.WorkspaceID != "ws-1" || in.ID != "acct-1" || in.AgentID == nil || *in.AgentID != "agent-2" || in.EnableWorkflow == nil || *in.EnableWorkflow ||
		in.WorkflowID != nil || in.DepartmentID != nil || in.EnableAgentResponses != nil {
		t.Fatalf("input = %+v", in)
	}
}

func TestDomainErrorsMapToHTTP(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{mldomain.ErrAccountNotFound, http.StatusNotFound, ""},
		{automationrefs.ErrNotInWorkspace, http.StatusUnprocessableEntity, "reference_not_in_workspace"},
		{&mldomain.TextCharsetError{Runes: []rune{'x'}}, http.StatusBadRequest, "text_charset"},
		{mldomain.ErrWorkspaceIDRequired, http.StatusBadRequest, ""},
		{mluc.ErrProbeInconclusive, http.StatusBadGateway, "mercadolivre_unreachable"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		h, _, accounts, _ := newTestHandler()
		accounts.err = tc.err
		rec := httptest.NewRecorder()
		req := mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{}`))), map[string]string{"id": "acct-1"})
		h.UpdateAccount(rec, req)
		if rec.Code != tc.status || (tc.code != "" && !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`)) {
			t.Errorf("%v: status = %d body = %s", tc.err, rec.Code, rec.Body)
		}
	}
	h, _, _, _ := newTestHandler()
	rec := httptest.NewRecorder()
	h.UpdateAccount(rec, mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{`))), map[string]string{"id": "acct-1"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json = %d", rec.Code)
	}
}

func TestDisconnectAnswersThePurgeDate(t *testing.T) {
	h, _, accounts, _ := newTestHandler()
	accounts.disconnect = &mluc.DisconnectOutput{PurgeAfter: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), Warning: "revoke_failed"}
	rec := httptest.NewRecorder()
	h.DisconnectAccount(rec, mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodDelete, "/x", nil)), map[string]string{"id": "acct-1"}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"purgeAfter":"2026-11-18T12:00:00Z"`) ||
		!strings.Contains(rec.Body.String(), `"status":"disconnected"`) || !strings.Contains(rec.Body.String(), `"warning":"revoke_failed"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestStartReturnsTheAuthorizeURLOrRedirects(t *testing.T) {
	h, c, _, _ := newTestHandler()
	c.start = &mluc.StartConnectOutput{AuthorizeURL: "https://auth.mercadolivre.com.br/authorization?x=1"}
	rec := httptest.NewRecorder()
	h.StartConnect(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/oauth/mercadolivre/start?popup=1&returnPath=/dashboard/mercadolivre-accounts", nil)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authorizeUrl":"https://auth.mercadolivre.com.br/authorization?x=1"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if !c.started.Popup || c.started.WorkspaceID != "ws-1" || c.started.ReturnPath != "/dashboard/mercadolivre-accounts" {
		t.Fatalf("start = %+v", c.started)
	}
	rec = httptest.NewRecorder()
	h.StartConnect(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/oauth/mercadolivre/start?redirect=1", nil)))
	if rec.Code != http.StatusFound {
		t.Fatalf("redirect = %d", rec.Code)
	}
}

func TestCallbackReportsThePopupResult(t *testing.T) {
	h, c, _, _ := newTestHandler()
	c.complete = &mluc.CompleteConnectOutput{Account: sampleAccount(), Popup: true, ReturnPath: "/dashboard/mercadolivre-accounts", Reconnected: true}
	rec := httptest.NewRecorder()
	h.HandleCallback(rec, httptest.NewRequest(http.MethodGet, "/oauth/mercadolivre/callback?code=TG-1&state=s", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"source":"mercadolivre-login"`) || !strings.Contains(body, `"status":"reconnected"`) ||
		!strings.Contains(body, `"nickname":"LOJA"`) || !strings.Contains(body, `"accountId":"acct-1"`) || !strings.Contains(body, `"https://app.example.com"`) {
		t.Fatalf("status = %d body = %s", rec.Code, body)
	}
	if c.got.Code != "TG-1" || c.got.State != "s" {
		t.Fatalf("input = %+v", c.got)
	}
}

func TestCallbackErrorsCarryAConnectErrorReason(t *testing.T) {
	cases := map[error]string{
		mldomain.ErrNotAccountAdmin:      "not_account_admin",
		mldomain.ErrSiteNotSupported:     "site_not_supported",
		mldomain.ErrPermissionMissing:    "missing_permission",
		mldomain.ErrAccountAlreadyLinked: "already_linked",
		mluc.ErrAuthorizationDeclined:    "authorization_declined",
		mluc.ErrProbeInconclusive:        "mercadolivre_unreachable",
		oauthstate.ErrExpiredState:       "expired_state",
		oauthstate.ErrReplayedState:      "invalid_state",
		errors.New("boom"):               "connect_failed",
	}
	for err, reason := range cases {
		h, c, _, _ := newTestHandler()
		c.err = fmt.Errorf("wrapped: %w", err)
		c.complete = &mluc.CompleteConnectOutput{Popup: true, ReturnPath: "/dashboard/mercadolivre-accounts"}
		rec := httptest.NewRecorder()
		h.HandleCallback(rec, httptest.NewRequest(http.MethodGet, "/oauth/mercadolivre/callback?error=x", nil))
		if !strings.Contains(rec.Body.String(), `"reason":"`+reason+`"`) || !strings.Contains(rec.Body.String(), `"status":"error"`) {
			t.Errorf("%v: body = %s", err, rec.Body)
		}
	}
	h, c, _, _ := newTestHandler()
	c.err = oauthstate.ErrInvalidState
	rec := httptest.NewRecorder()
	h.HandleCallback(rec, httptest.NewRequest(http.MethodGet, "/oauth/mercadolivre/callback?state=bad", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("an unreadable state redirects: %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Host != "app.example.com" || loc.Path != "/dashboard/mercadolivre-accounts" || loc.Query().Get("mercadolivre") != "error" || loc.Query().Get("reason") != "invalid_state" {
		t.Fatalf("location = %s", loc)
	}
}

func TestHealthCheckAnswersTheAccount(t *testing.T) {
	h, _, _, health := newTestHandler()
	rec := httptest.NewRecorder()
	h.HealthCheck(rec, mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodPost, "/x", nil)), map[string]string{"id": "acct-1"}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lastHealthCheckAt"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	health.err = mldomain.ErrAccountNotFound
	rec = httptest.NewRecorder()
	h.HealthCheck(rec, mux.SetURLVars(withWorkspace(httptest.NewRequest(http.MethodPost, "/x", nil)), map[string]string{"id": "acct-1"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
