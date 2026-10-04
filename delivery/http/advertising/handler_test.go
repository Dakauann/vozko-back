package advertisinghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

func requestWithAccount(target string) *http.Request {
	return mux.SetURLVars(httptest.NewRequest(http.MethodGet, target, nil), map[string]string{"id": "acc-1"})
}

func issueOf(t *testing.T, err error) advertising.FieldIssue {
	t.Helper()
	var invalid *advertising.ValidationError
	if !errors.As(err, &invalid) || len(invalid.Issues) != 1 {
		t.Fatalf("want one field issue, got %v", err)
	}
	return invalid.Issues[0]
}

func TestReportQueryLeavesDefaultsToTheUseCase(t *testing.T) {
	q, err := reportQuery(requestWithAccount("/ads/accounts/acc-1/report"))
	if err != nil {
		t.Fatal(err)
	}
	if q.AccountID != "acc-1" || q.Level != "" || q.Compare || !q.Range.Since.IsZero() {
		t.Fatalf("got %+v", q)
	}
}

func TestReportQueryReadsEveryFilter(t *testing.T) {
	q, err := reportQuery(requestWithAccount("/x?level=ad&since=2026-09-01&until=2026-09-30&campaignIds=c1,%20c2,&adSetIds=s1&search=%20promo%20&compare=1"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Level != advertising.LevelAd || !q.Compare || q.Search != "promo" ||
		!reflect.DeepEqual(q.CampaignIDs, []string{"c1", "c2"}) || !reflect.DeepEqual(q.AdSetIDs, []string{"s1"}) ||
		q.Range.Since.Format(advertising.DayLayout) != "2026-09-01" || q.Range.Until.Format(advertising.DayLayout) != "2026-09-30" {
		t.Fatalf("got %+v", q)
	}
}

func TestReportQueryRejectsUnknownValues(t *testing.T) {
	cases := map[string]string{
		"/x?compare=yes": "compare",
	}
	for target, field := range cases {
		_, err := reportQuery(requestWithAccount(target))
		if got := issueOf(t, err); got.Field != field || got.Code != "invalid" {
			t.Errorf("%s: got %+v", target, got)
		}
	}
	if _, err := reportQuery(requestWithAccount("/x?since=2026-09-30&until=2026-09-01")); !errors.Is(err, advertising.ErrInvalidRange) {
		t.Errorf("reversed range: got %v", err)
	}
}

func TestLiveQuerySplitsBreakdownsAndWindows(t *testing.T) {
	q, err := liveQuery(requestWithAccount("/x?level=adset&objectIds=a,b&breakdowns=age,gender&windows=7d_click,1d_view"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Level != advertising.LevelAdSet || q.AccountID != "acc-1" ||
		!reflect.DeepEqual(q.ObjectIDs, []string{"a", "b"}) ||
		!reflect.DeepEqual(q.Breakdowns, []advertising.Breakdown{advertising.BreakdownAge, advertising.BreakdownGender}) ||
		!reflect.DeepEqual(q.Windows, []advertising.AttributionWindow{advertising.Window7DayClick, advertising.Window1DayView}) {
		t.Fatalf("got %+v", q)
	}
}

func TestIntQueryLeavesBoundsToTheUseCase(t *testing.T) {
	read := func(target string) (int, error) {
		return intQuery(httptest.NewRequest(http.MethodGet, target, nil), "limit")
	}
	if v, err := read("/x"); err != nil || v != 0 {
		t.Fatalf("absent: %d %v", v, err)
	}
	if v, err := read("/x?limit=-3"); err != nil || v != -3 {
		t.Fatalf("negative: %d %v", v, err)
	}
	for _, bad := range []string{"ten", "1.5"} {
		if _, err := read("/x?limit=" + bad); issueOf(t, err).Field != "limit" {
			t.Errorf("limit=%s accepted", bad)
		}
	}
}

func TestSpendCapRequestTellsNullFromMissing(t *testing.T) {
	cases := []struct {
		body    string
		present bool
		value   *int64
	}{
		{`{}`, false, nil},
		{`{"amount":null}`, true, nil},
		{`{"amount":150000}`, true, int64Ptr(150000)},
	}
	for _, c := range cases {
		var req SpendCapRequest
		if err := json.Unmarshal([]byte(c.body), &req); err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		if req.Amount.present != c.present || !reflect.DeepEqual(req.Amount.value, c.value) {
			t.Errorf("%s: got present=%v value=%v", c.body, req.Amount.present, req.Amount.value)
		}
	}
	var req SpendCapRequest
	if err := json.Unmarshal([]byte(`{"amount":"lots"}`), &req); err == nil {
		t.Fatal("a non-number amount was accepted")
	}
}

func serve(handler http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func expectedFields(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var payload struct {
		Code     string            `json:"code"`
		Expected map[string]string `json:"expected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "invalid_draft" {
		t.Fatalf("code %q", payload.Code)
	}
	return payload.Expected
}

func TestHandlersRejectIncompleteRequestsBeforeCallingMeta(t *testing.T) {
	h := NewHandler(Deps{})
	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		target  string
		body    string
		field   string
		code    string
	}{
		{"spend cap without amount", h.SetSpendCap, http.MethodPut, "/ads/accounts/a/spend-cap", `{}`, "amount", "required"},
		{"rule status without enabled", h.SetRuleStatus, http.MethodPost, "/ads/rules/r/status?accountId=a", `{}`, "enabled", "required"},
		{"rule status without account", h.SetRuleStatus, http.MethodPost, "/ads/rules/r/status", `{"enabled":true}`, "accountId", "required"},
		{"rule delete without account", h.DeleteRule, http.MethodDelete, "/ads/rules/r", ``, "accountId", "required"},
		{"rule history without account", h.RuleHistory, http.MethodGet, "/ads/rules/r/history?accountId=%20", ``, "accountId", "required"},
		{"audience delete without account", h.DeleteAudience, http.MethodDelete, "/ads/audiences/x", ``, "accountId", "required"},
		{"csv with unknown compare", h.ReportCSV, http.MethodGet, "/ads/accounts/a/report.csv?compare=maybe", ``, "compare", "invalid"},
		{"budget minimum without goal", h.BudgetMinimum, http.MethodGet, "/ads/accounts/a/budget-minimum", ``, "goal", "required"},
		{"budget minimum with a bad bid", h.BudgetMinimum, http.MethodGet, "/ads/accounts/a/budget-minimum?goal=LINK_CLICKS&bidAmount=x", ``, "bidAmount", "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serve(c.handler, c.method, c.target, c.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if got := expectedFields(t, rec); got[c.field] != c.code {
				t.Fatalf("got %v, want %s=%s", got, c.field, c.code)
			}
		})
	}
}

func TestMalformedBodiesAreBadRequests(t *testing.T) {
	h := NewHandler(Deps{})
	for name, handler := range map[string]http.HandlerFunc{
		"spend cap": h.SetSpendCap, "budget": h.SetBudget, "edit": h.EditObject, "rule": h.CreateRule, "rule status": h.SetRuleStatus,
	} {
		if rec := serve(handler, http.MethodPost, "/x", `{"amount":"lots","enabled":"yes","edit":1,"name":7}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

type oneDraft struct {
	advertising.SavedDraftRepository
	draft *advertising.SavedDraft
}

func (o oneDraft) Find(context.Context, string, string) (*advertising.SavedDraft, error) {
	return o.draft, nil
}

func (o oneDraft) ClaimForPublish(_ context.Context, d *advertising.SavedDraft, jobID string) error {
	d.JobID = jobID
	d.Version++
	return nil
}

type actorPublisher struct{ actor advertising.Actor }

func (p *actorPublisher) Publish(_ context.Context, in adsuc.PublishInput) (*advertising.PublishJob, error) {
	p.actor = in.Actor
	return &advertising.PublishJob{ID: in.JobID, Status: advertising.JobRunning}, nil
}

func TestPublishingADraftOnTheScreenIsRecordedAsThePerson(t *testing.T) {
	publisher := &actorPublisher{}
	drafts := oneDraft{draft: &advertising.SavedDraft{ID: "d-1", Version: 3}}
	h := NewHandler(Deps{Drafts: adsuc.NewDraftsUseCase(drafts, nil, nil, nil, publisher)})
	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/ads/drafts/d-1/publish", strings.NewReader(`{"version":3}`)), map[string]string{"id": "d-1"})
	rec := httptest.NewRecorder()
	h.PublishDraft(rec, req)
	if rec.Code != http.StatusCreated || publisher.actor != advertising.ActorPerson {
		t.Fatalf("status %d actor %q: %s", rec.Code, publisher.actor, rec.Body.String())
	}
}
