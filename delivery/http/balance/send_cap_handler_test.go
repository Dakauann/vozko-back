package balance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	balancedomain "vozko/domain/balance"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type listSendCapsStub struct {
	gotActor balancedomain.SendCapActor
	gotLevel balancedomain.SendCapLevel
	listing  *balancedomain.SendCapListing
	err      error
}

func (s *listSendCapsStub) Execute(actor balancedomain.SendCapActor, level balancedomain.SendCapLevel) (*balancedomain.SendCapListing, error) {
	s.gotActor, s.gotLevel = actor, level
	return s.listing, s.err
}

type setSendCapStub struct {
	gotActor balancedomain.SendCapActor
	gotWS    string
	gotLimit int64
	err      error
}

func (s *setSendCapStub) Execute(actor balancedomain.SendCapActor, workspaceID string, limit int64) (*balancedomain.MonthlySendCap, error) {
	s.gotActor, s.gotWS, s.gotLimit = actor, workspaceID, limit
	if s.err != nil {
		return nil, s.err
	}
	return &balancedomain.MonthlySendCap{WorkspaceID: workspaceID, Limit: limit}, nil
}

type unlockSendCapStub struct {
	gotActor balancedomain.SendCapActor
	gotInput balancedomain.UnlockMonthlySendCapInput
	err      error
}

func (s *unlockSendCapStub) Execute(actor balancedomain.SendCapActor, input balancedomain.UnlockMonthlySendCapInput) (*balancedomain.MonthlySendCap, error) {
	s.gotActor, s.gotInput = actor, input
	if s.err != nil || input.Limit == nil {
		return nil, s.err
	}
	return &balancedomain.MonthlySendCap{WorkspaceID: input.WorkspaceID, Limit: *input.Limit}, nil
}

func sendCapRequest(method, target, body string, claims *auth.Claims) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, claims))
	}
	return req
}

func serveSendCaps(h *SendCapHandler, req *http.Request) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	RegisterSendCapAdminRoutes(router, h)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var adminClaims = &auth.Claims{UserID: "root-1", Email: "dakauannc@gmail.com", Role: "admin"}

func TestSendCapHandler_ListBuildsActorFromClaimsAndRendersUsage(t *testing.T) {
	updatedAt := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	list := &listSendCapsStub{listing: &balancedomain.SendCapListing{
		MonthStart: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC),
		CanUnlock:  true,
		Items: []balancedomain.SendCapUsage{{
			WorkspaceName: "Acme",
			Used:          85,
			Cap:           balancedomain.MonthlySendCap{WorkspaceID: "ws-1", Limit: 100, UpdatedBy: "admin-1", UpdatedAt: updatedAt},
		}},
	}}
	h := NewSendCapHandler(list, &setSendCapStub{}, &unlockSendCapStub{})

	rec := serveSendCaps(h, sendCapRequest(http.MethodGet, "/admin/send-caps?level=near", "", adminClaims))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if list.gotActor != (balancedomain.SendCapActor{UserID: "root-1", Email: "dakauannc@gmail.com", SystemAdmin: true}) || list.gotLevel != balancedomain.SendCapLevelNear {
		t.Fatalf("unexpected actor %+v level %q", list.gotActor, list.gotLevel)
	}
	var body SendCapListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.CanUnlock || len(body.Items) != 1 {
		t.Fatalf("unexpected body %+v", body)
	}
	item := body.Items[0]
	if item.WorkspaceID != "ws-1" || item.WorkspaceName != "Acme" || item.Limit != 100 || item.Used != 85 || item.Remaining != 15 || item.Level != "near" {
		t.Fatalf("unexpected item %+v", item)
	}
}

func TestSendCapHandler_ListRejectsUnknownLevel(t *testing.T) {
	h := NewSendCapHandler(&listSendCapsStub{}, &setSendCapStub{}, &unlockSendCapStub{})
	rec := serveSendCaps(h, sendCapRequest(http.MethodGet, "/admin/send-caps?level=full", "", adminClaims))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSendCapHandler_RequiresClaims(t *testing.T) {
	h := NewSendCapHandler(&listSendCapsStub{}, &setSendCapStub{}, &unlockSendCapStub{})
	rec := serveSendCaps(h, sendCapRequest(http.MethodGet, "/admin/send-caps", "", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSendCapHandler_Set(t *testing.T) {
	set := &setSendCapStub{}
	h := NewSendCapHandler(&listSendCapsStub{}, set, &unlockSendCapStub{})

	rec := serveSendCaps(h, sendCapRequest(http.MethodPut, "/admin/send-caps/ws-1", `{"limit":1000}`, adminClaims))

	if rec.Code != http.StatusOK || set.gotWS != "ws-1" || set.gotLimit != 1000 || set.gotActor.UserID != "root-1" {
		t.Fatalf("status %d, ws %q, limit %d", rec.Code, set.gotWS, set.gotLimit)
	}
	var body SendCapChangeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Limit == nil || *body.Limit != 1000 {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestSendCapHandler_SetRejectsMalformedBody(t *testing.T) {
	set := &setSendCapStub{}
	h := NewSendCapHandler(&listSendCapsStub{}, set, &unlockSendCapStub{})
	for _, body := range []string{`{"limit":"ten"}`, `nope`, `{}`} {
		rec := serveSendCaps(h, sendCapRequest(http.MethodPut, "/admin/send-caps/ws-1", body, adminClaims))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
	if set.gotWS != "" {
		t.Fatal("a malformed body never reaches the use case")
	}
}

func TestSendCapHandler_UnlockPassesLimitAndCode(t *testing.T) {
	unlock := &unlockSendCapStub{}
	h := NewSendCapHandler(&listSendCapsStub{}, &setSendCapStub{}, unlock)

	rec := serveSendCaps(h, sendCapRequest(http.MethodPost, "/admin/send-caps/ws-1/unlock", `{"limit":5000,"code":"1234"}`, adminClaims))
	if rec.Code != http.StatusOK || unlock.gotInput.WorkspaceID != "ws-1" || unlock.gotInput.Code != "1234" || unlock.gotInput.Limit == nil || *unlock.gotInput.Limit != 5000 {
		t.Fatalf("status %d, input %+v", rec.Code, unlock.gotInput)
	}

	rec = serveSendCaps(h, sendCapRequest(http.MethodPost, "/admin/send-caps/ws-1/unlock", `{"removeCap":true,"code":"1234"}`, adminClaims))
	var body SendCapChangeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || body.Limit != nil || body.WorkspaceID != "ws-1" {
		t.Fatalf("removing the cap answers with no limit, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSendCapHandler_ErrorMapping(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{balancedomain.ErrSendCapForbidden, http.StatusForbidden, "forbidden"},
		{balancedomain.ErrSendCapUnlockRequired, http.StatusForbidden, "unlock_required"},
		{balancedomain.ErrInvalidUnlockCode, http.StatusForbidden, "invalid_unlock_code"},
		{balancedomain.ErrInvalidSendCapLimit, http.StatusBadRequest, "invalid_limit"},
		{balancedomain.ErrMonthlySendCapNotFound, http.StatusNotFound, "send_cap_not_found"},
		{workspace.ErrWorkspaceNotFound, http.StatusNotFound, "workspace_not_found"},
		{errors.New("db down"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			h := NewSendCapHandler(&listSendCapsStub{}, &setSendCapStub{err: tc.err}, &unlockSendCapStub{err: tc.err})
			rec := serveSendCaps(h, sendCapRequest(http.MethodPost, "/admin/send-caps/ws-1/unlock", `{"limit":10,"code":"1234"}`, adminClaims))
			if rec.Code != tc.wantStatus || !strings.Contains(rec.Body.String(), `"code":"`+tc.wantCode+`"`) {
				t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), tc.wantStatus, tc.wantCode)
			}
			if tc.wantStatus == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "db down") {
				t.Fatal("internal errors must not leak their detail")
			}
		})
	}
}

func TestSendCapHandler_UnlockNeedsExactlyOneOfLimitOrRemoval(t *testing.T) {
	unlock := &unlockSendCapStub{}
	h := NewSendCapHandler(&listSendCapsStub{}, &setSendCapStub{}, unlock)
	for _, body := range []string{`{"code":"1234"}`, `{"limit":null,"code":"1234"}`, `{"limit":10,"removeCap":true,"code":"1234"}`, `nope`} {
		rec := serveSendCaps(h, sendCapRequest(http.MethodPost, "/admin/send-caps/ws-1/unlock", body, adminClaims))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
	if unlock.gotInput.WorkspaceID != "" {
		t.Fatal("an ambiguous unlock never reaches the use case")
	}
}
