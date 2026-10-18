package campaignexithttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	campaignexit_usecase "vozko/usecases/campaignexit"
)

type fakeExits struct {
	workspaceID string
	numbers     []string
	exits       campaign.AudienceExits
	err         error
}

func (f *fakeExits) AudienceExits(_ context.Context, workspaceID string, numbers []string) (campaign.AudienceExits, error) {
	f.workspaceID, f.numbers = workspaceID, numbers
	return f.exits, f.err
}

func serve(t *testing.T, exits AudienceExits, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	var gated []workspace_domain.Action
	RegisterProtectedRoutes(router, NewHandler(exits), func(_ workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		gated = append(gated, action)
		return h
	})
	if len(gated) != 1 || gated[0] != workspace_domain.ActionCreate {
		t.Fatalf("the route is gated by %v, want campaign creation", gated)
	}
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/campaigns/exit-audience", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestTheAudienceExitsAnswerHowManyOfTheListDoNotReceive(t *testing.T) {
	exits := &fakeExits{exits: campaign.AudienceExits{OptedOut: 30, Blocked: 7}}
	rec := serve(t, exits, `{"numbers":["5511987650001","5511987650002"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"optedOut":30`, `"blocked":7`, `"notReceiving":37`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body misses %s: %s", want, rec.Body)
		}
	}
	if exits.workspaceID != "ws-1" || len(exits.numbers) != 2 {
		t.Fatalf("asked %q %v", exits.workspaceID, exits.numbers)
	}
}

func TestTheAudienceExitsRefuseWithCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{campaignexit_usecase.ErrTooManyNumbers, http.StatusRequestEntityTooLarge, "campaign_exit_too_many_numbers"},
		{campaignexit_usecase.ErrUnavailable, http.StatusServiceUnavailable, "campaign_exit_unavailable"},
		{cache.ErrGateBusy, http.StatusServiceUnavailable, ""},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		rec := serve(t, &fakeExits{err: tc.err}, `{"numbers":["5511987650001"]}`)
		if rec.Code != tc.status || (tc.code != "" && !strings.Contains(rec.Body.String(), tc.code)) {
			t.Errorf("%v = %d %s", tc.err, rec.Code, rec.Body)
		}
	}
	if rec := serve(t, &fakeExits{}, `{"numbers":[],"campaignId":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key = %d", rec.Code)
	}
	if rec := serve(t, nil, `{"numbers":[]}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without the service = %d", rec.Code)
	}
}
