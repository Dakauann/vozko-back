package crmbulk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/infra/http/middleware"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type stubService struct {
	got        crmbulk_usecase.BulkInput
	result     crmbulk_usecase.BulkResult
	err        error
	countScope selection.Scope
	countOf    crmfilter.Filter
	matched    int
}

func (s *stubService) BulkApply(_ context.Context, in crmbulk_usecase.BulkInput) (crmbulk_usecase.BulkResult, error) {
	s.got = in
	if s.err != nil {
		return crmbulk_usecase.BulkResult{}, s.err
	}
	return s.result, nil
}

func (s *stubService) Count(_ context.Context, scope selection.Scope, filter crmfilter.Filter) (int, string, error) {
	s.countScope, s.countOf = scope, filter
	return s.matched, selection.Fingerprint(filter), s.err
}

func call(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/crm/bulk", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"})
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	return rec
}

const unreadFilterJSON = `{"groups":[{"conjunction":"and","predicates":[{"field":"unread","operator":"is_true"}]}]}`

func TestBulk_ForwardsTheSelectionAsSentWithoutInventingOne(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		want      selection.Selection
		targets   int
		hasFilter bool
	}{
		{"picked targets", `{"action":"move_stage","value":"s","targets":[{"entryId":"e1","entryType":"whatsapp"}]}`, selection.Selection{}, 1, false},
		{"a legacy filter gets no server fingerprint", `{"action":"move_stage","value":"s","filter":` + unreadFilterJSON + `}`, selection.Selection{}, 0, true},
		{"an explicit confirmed selection", `{"action":"move_stage","value":"s","mode":"all_matching","fingerprint":"abc","expectedCount":340,"excludeIds":["e9"],"filter":` + unreadFilterJSON + `}`,
			selection.Selection{Mode: selection.ModeAllMatching, Fingerprint: "abc", ExpectedCount: 340, ExcludeIDs: []string{"e9"}}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{}
			rec := call(t, NewCRMBulkHandler(svc).Bulk, tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			got := svc.got.Selection
			if got.Mode != tc.want.Mode || got.Fingerprint != tc.want.Fingerprint || got.ExpectedCount != tc.want.ExpectedCount || fmt.Sprint(got.ExcludeIDs) != fmt.Sprint(tc.want.ExcludeIDs) {
				t.Fatalf("selection %+v, want %+v", got, tc.want)
			}
			if (got.Filter != nil) != tc.hasFilter || len(svc.got.Targets) != tc.targets {
				t.Fatalf("filter or targets not forwarded: %+v", svc.got)
			}
			if svc.got.WorkspaceID != "ws-1" || svc.got.ActorID != "u-1" {
				t.Fatalf("actor not forwarded: %+v", svc.got)
			}
		})
	}
}

func TestBulkAndCount_AnswerBusyWithRetryAfter(t *testing.T) {
	busy := fmt.Errorf("crmbulk: count selection: %w", cache.ErrGateBusy)
	for name, h := range map[string]http.HandlerFunc{
		"bulk":  NewCRMBulkHandler(&stubService{err: busy}).Bulk,
		"count": NewCRMBulkHandler(&stubService{err: busy}).Count,
	} {
		t.Run(name, func(t *testing.T) {
			rec := call(t, h, `{"action":"move_stage","value":"s","targets":[{"entryId":"e1","entryType":"whatsapp"}]}`)
			if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
				t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestBulk_MapsRefusalsToStatusAndCode(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"forbidden", crmbulk_usecase.ErrForbidden, http.StatusForbidden, "forbidden"},
		{"unknown action", crmbulk_usecase.ErrUnknownAction, http.StatusBadRequest, "unknown_action"},
		{"count changed", &selection.CountChangedError{Expected: 10, Matched: 12}, http.StatusConflict, "selection_changed"},
		{"resolver missing", selection.ErrResolverUnavailable, http.StatusServiceUnavailable, "selection_unavailable"},
		{"invalid filter", crmfilter.ErrUnknownField, http.StatusBadRequest, "invalid_filter"},
		{"filter the object cannot apply", fmt.Errorf("compile: %w", crmfilter.ErrNotApplicable), http.StatusBadRequest, "invalid_filter"},
		{"scope refused", fmt.Errorf("resolve: %w", selection.ErrScopeDenied), http.StatusForbidden, "selection_scope_denied"},
		{"selection refused", selection.ErrFingerprintMismatch, http.StatusBadRequest, "selection_fingerprint_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(t, NewCRMBulkHandler(&stubService{err: tc.err}).Bulk,
				`{"action":"move_stage","value":"s","targets":[{"entryId":"e1","entryType":"whatsapp"}]}`)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d", rec.Code, tc.status)
			}
			var body struct {
				Code    string `json:"code"`
				Matched int    `json:"matched"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != tc.code {
				t.Fatalf("code %q, want %q (%v)", body.Code, tc.code, err)
			}
			if tc.status == http.StatusConflict && body.Matched != 12 {
				t.Fatalf("the conflict must carry the new count, got %d", body.Matched)
			}
		})
	}
}

func TestBulk_ResultBody(t *testing.T) {
	svc := &stubService{result: crmbulk_usecase.BulkResult{Matched: 3, Eligible: 3, Succeeded: 2, Failed: []selection.Failure{{ID: "e3", Error: "boom"}}}}
	rec := call(t, NewCRMBulkHandler(svc).Bulk, `{"action":"move_stage","value":"s","targets":[{"entryId":"e1","entryType":"whatsapp"}]}`)
	var body BulkResultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Succeeded != 2 || body.Matched != 3 || len(body.Failed) != 1 || body.Failed[0].EntryID != "e3" {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestCount_ReturnsMatchedAndFingerprint(t *testing.T) {
	svc := &stubService{matched: 340}
	rec := call(t, NewCRMBulkHandler(svc).Count, `{"filter":`+unreadFilterJSON+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body BulkCountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Matched != 340 || body.Fingerprint != selection.Fingerprint(svc.countOf) || svc.countScope.ActorID != "u-1" {
		t.Fatalf("unexpected body %+v scope %+v", body, svc.countScope)
	}
}

func TestBulk_RefusesWithoutWorkspace(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/crm/bulk", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"}))
	rec := httptest.NewRecorder()
	NewCRMBulkHandler(&stubService{}).Bulk(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}
