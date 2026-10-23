package emailoutreach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/emailpricing"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	"vozko/domain/balance"
	"vozko/domain/conversation"
	es "vozko/domain/emailsending"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/emailoutreach"
	"vozko/usecases/transactionalemail"
)

type startFake struct {
	inputs  []emailoutreach.Input
	started emailoutreach.Started
	err     error
}

func (f *startFake) Execute(_ context.Context, in emailoutreach.Input) (emailoutreach.Started, error) {
	f.inputs = append(f.inputs, in)
	return f.started, f.err
}

type pricesFake struct {
	result balance_usecase.EmailPriceListResult
	err    error
}

func (f pricesFake) Execute(context.Context, string) (balance_usecase.EmailPriceListResult, error) {
	return f.result, f.err
}

type scopeFake struct {
	scope conversation.DepartmentAccessScope
}

func (s scopeFake) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return s.scope, true
}

func authed(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	return r.WithContext(ctx)
}

func startRequest(body string) *http.Request {
	return authed(httptest.NewRequest(http.MethodPost, "/email/outreach/conversations", strings.NewReader(body)))
}

const validBody = `{"lead_id":"lead-1","sender_id":"s-1","subject":"Oi {{first_name}}","text":"Corpo","idempotency_key":"k-1"}`

type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		if h == nil {
			http.Error(w, "handler must not be nil", http.StatusInternalServerError)
		}
	}
}

func TestRegisterProtectedRoutesAppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)
	cases := []struct {
		method, path, want, why string
	}{
		{http.MethodPost, "/email/outreach/conversations", "email_marketing:start", "sending spends balance and contacts a customer, like a launch"},
		{http.MethodGet, "/email/outreach/quote", "email_marketing:read", "showing a price must not require the permission to spend it"},
	}
	for _, tc := range cases {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
		if got := ac.calls[tc.method+" "+tc.path]; got != tc.want {
			t.Errorf("%s %s gate = %q, want %q: %s", tc.method, tc.path, got, tc.want, tc.why)
		}
	}
}

func TestRegisterProtectedRoutesWithoutAHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, nil, ac.fn)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/outreach/conversations", nil))
	if rec.Code != http.StatusNotFound || len(ac.calls) != 0 {
		t.Fatalf("status %d calls %v", rec.Code, ac.calls)
	}
}

func TestStartPassesTheOperatorAndAnswersTheConversation(t *testing.T) {
	start := &startFake{started: emailoutreach.Started{ConversationID: "conv-1", EntryType: "email", MessageID: "msg-1", Recorded: true}}
	restricted := conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-1"}}
	h := NewHandler(HandlerDeps{Start: start, Prices: pricesFake{}, Departments: scopeFake{scope: restricted}})
	rec := httptest.NewRecorder()
	h.StartConversation(rec, startRequest(validBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	in := start.inputs[0]
	if in.WorkspaceID != "ws1" || in.UserID != "u1" || in.IsAdmin || !in.Departments.Restrict || in.LeadID != "lead-1" || in.SenderID != "s-1" ||
		in.Subject != "Oi {{first_name}}" || in.Text != "Corpo" || in.DesignID != "" || in.IdempotencyKey != "k-1" {
		t.Fatalf("input = %+v", in)
	}
	var body StartedConversationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ConversationID != "conv-1" || body.EntryType != "email" || body.MessageID != "msg-1" || !body.Recorded || body.Repeated {
		t.Fatalf("body %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"conversation_id":"conv-1"`) || !strings.Contains(rec.Body.String(), `"entry_type":"email"`) {
		t.Fatalf("snake case body %s", rec.Body.String())
	}
}

func TestStartRepeatAnswersOK(t *testing.T) {
	start := &startFake{started: emailoutreach.Started{ConversationID: "conv-1", EntryType: "email", MessageID: "msg-1", Repeated: true, Recorded: true}}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: start, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(validBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"repeated":true`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestStartRefusesAnUnknownField(t *testing.T) {
	start := &startFake{}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: start, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(`{"phone":"1"}`))
	if rec.Code != http.StatusBadRequest || len(start.inputs) != 0 {
		t.Fatalf("status %d", rec.Code)
	}
}

func everyRefusal() []error {
	codes := []string{
		transactionalemail.CodeRequestInvalid, transactionalemail.CodeMessageInvalid, transactionalemail.CodeSubjectInvalid,
		transactionalemail.CodeAddressInvalid, transactionalemail.CodeRecipientMissing, transactionalemail.CodeLeadNotFound,
		transactionalemail.CodeLeadBlocked, transactionalemail.CodeLeadOptedOut, transactionalemail.CodeLeadNoEmail,
		transactionalemail.CodeDesignNotFound, transactionalemail.CodeBalanceInsufficient, transactionalemail.CodeChargeFailed,
		transactionalemail.CodeRecipientSuppressed, transactionalemail.CodeSenderMissing, transactionalemail.CodeSenderNotVerified,
		transactionalemail.CodeAccountPaused, transactionalemail.CodeAccountReactivation, transactionalemail.CodeDailyCapReached,
		transactionalemail.CodeSendInProgress, transactionalemail.CodeUnavailable, transactionalemail.CodeSendThrottled,
		emailoutreach.CodeIdempotencyKeyRequired, emailoutreach.CodeForbidden,
	}
	out := make([]error, 0, len(codes)+1)
	for _, code := range codes {
		out = append(out, &transactionalemail.Refusal{Code: code, Err: errors.New(code)})
	}
	return append(out, transactionalemail.ErrSentByInvalid)
}

func TestEveryRefusalAnswersItsCodeAndNeverAServerError(t *testing.T) {
	for _, err := range everyRefusal() {
		start := &startFake{err: fmt.Errorf("start: %w", err)}
		rec := httptest.NewRecorder()
		NewHandler(HandlerDeps{Start: start, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(validBody))
		want := transactionalemail.CodeOf(err)
		if got := responsetest.CodeOf(t, rec); got != want || rec.Code >= http.StatusInternalServerError && rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%v: status %d code %s, want code %s", err, rec.Code, got, want)
		}
	}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: &startFake{err: errors.New("provider down")}, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(validBody))
	if rec.Code != http.StatusBadGateway || responsetest.CodeOf(t, rec) != transactionalemail.CodeSendFailed {
		t.Fatalf("an unknown failure: status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAShortWalletCarriesTheMissingAmount(t *testing.T) {
	short := &transactionalemail.Refusal{Code: transactionalemail.CodeBalanceInsufficient, Err: &balance_usecase.InsufficientEmailBalanceError{MissingMicros: 417}}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: &startFake{err: short}, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(validBody))
	var body struct {
		Code    string `json:"code"`
		Details struct {
			MissingMicros int64 `json:"missing_micros"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusPaymentRequired || body.Code != "email_balance_insufficient" || body.Details.MissingMicros != 417 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestQuoteAnswersThePriceOfOneEmail(t *testing.T) {
	prices := pricesFake{result: balance_usecase.EmailPriceListResult{ExchangeRate: 6, Send: balance_usecase.EmailPrice{Service: "send", UnitMicros: 583, UnitBRL: 0.003498}}}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: &startFake{}, Prices: prices, Departments: scopeFake{}}).Quote(rec, authed(httptest.NewRequest(http.MethodGet, "/email/outreach/quote", nil)))
	var body QuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || body.UnitMicros != 583 || body.UnitBRL != 0.003498 || body.ExchangeRate != 6 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestQuoteFailsClosedOnAnUnreadableRate(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{balance_usecase.ErrExchangeRateUnavailable, http.StatusServiceUnavailable, "email_exchange_rate_unavailable"},
		{balance.ErrPriceUnavailable, http.StatusServiceUnavailable, "email_pricing_unavailable"},
		{errors.New("db down"), http.StatusInternalServerError, "email_pricing_failed"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		NewHandler(HandlerDeps{Start: &startFake{}, Prices: pricesFake{err: tc.err}, Departments: scopeFake{}}).Quote(rec, authed(httptest.NewRequest(http.MethodGet, "/email/outreach/quote", nil)))
		if rec.Code != tc.status || responsetest.CodeOf(t, rec) != tc.code || strings.Contains(rec.Body.String(), "unit_micros") {
			t.Errorf("%v: status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestOutreachErrorCodesMatchTheGolden(t *testing.T) {
	codes := append(RefusalCodes(), emailpricing.RefusalCodes()...)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, "update testdata/error_codes.golden and translate emailMarketing.errors.<code> in the pt, en, es and de catalogs of vozko-front")
}

func TestOutreachRefusalsHaveStableCodesAndMessages(t *testing.T) {
	responsetest.RequireStableCodes(t, append(outreachRefusals, refusalSendFailed))
}

func TestAThrottledSendAsksToRetryAfterWholeSeconds(t *testing.T) {
	throttled := &transactionalemail.Refusal{Code: transactionalemail.CodeSendThrottled, Err: &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: rate", RetryAfter: 1200 * time.Millisecond}}
	rec := httptest.NewRecorder()
	NewHandler(HandlerDeps{Start: &startFake{err: throttled}, Prices: pricesFake{}, Departments: scopeFake{}}).StartConversation(rec, startRequest(validBody))
	var body struct {
		Code    string `json:"code"`
		Details struct {
			RetryAfterSeconds int64 `json:"retry_after_seconds"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusTooManyRequests || body.Code != "email_send_throttled" ||
		body.Details.RetryAfterSeconds != 2 || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status %d header %q body %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}
