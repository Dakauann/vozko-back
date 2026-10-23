package emailapihttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/apikeys"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/apikey"
	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
	wh "vozko/domain/emailwebhook"
	workspace_domain "vozko/domain/workspace"
	"vozko/pkg/codegolden"
	apikey_usecase "vozko/usecases/apikey"
	balance_usecase "vozko/usecases/balance"
	emailapi_usecase "vozko/usecases/emailapi"
	emailwebhook_usecase "vozko/usecases/emailwebhook"
	tx "vozko/usecases/transactionalemail"
)

type keysFake map[string]apikey.Principal

func (k keysFake) Authenticate(_ context.Context, token string, scope apikey.Scope) (apikey.Principal, error) {
	if token == "" {
		return apikey.Principal{}, apikey_usecase.ErrUnauthenticated
	}
	if token == "down" {
		return apikey.Principal{}, apikey_usecase.ErrUnavailable
	}
	if token == "revoked" {
		return apikey.Principal{}, apikey.ErrRevoked
	}
	p, ok := k[token]
	if !ok {
		return apikey.Principal{}, apikey_usecase.ErrInvalid
	}
	if !p.Allows(scope) {
		return apikey.Principal{}, apikey_usecase.ErrScopeMissing
	}
	return p, nil
}

type limiterFake struct {
	refuse map[string]bool
	err    error
}

func (l limiterFake) Allow(key string) (bool, time.Duration, error) {
	if l.err != nil {
		return false, 0, l.err
	}
	if l.refuse[key] {
		return false, 1500 * time.Millisecond, nil
	}
	return true, 0, nil
}

type senderFake struct {
	got    emailapi.Request
	result emailapi_usecase.Result
	err    error
}

func (s *senderFake) Send(_ context.Context, req emailapi.Request) (emailapi_usecase.Result, error) {
	s.got = req
	return s.result, s.err
}

type logFake struct {
	got   emailapi.LogQuery
	page  emailapi.Page[emailapi.LogEntry]
	err   error
	asked string
}

func (l *logFake) List(_ context.Context, q emailapi.LogQuery) (emailapi.Page[emailapi.LogEntry], error) {
	l.got = q
	return l.page, l.err
}

func (l *logFake) Detail(_ context.Context, ws, id string) (emailapi.Detail, error) {
	l.asked = ws + "/" + id
	return emailapi.Detail{}, l.err
}

type webhooksFake struct{ err error }

func (f webhooksFake) Create(context.Context, emailwebhook_usecase.CreateInput) (emailwebhook_usecase.WithSecret, error) {
	return emailwebhook_usecase.WithSecret{Secret: "whsec_x"}, f.err
}
func (f webhooksFake) Update(context.Context, emailwebhook_usecase.UpdateInput) (wh.Endpoint, error) {
	return wh.Endpoint{}, f.err
}
func (f webhooksFake) Delete(context.Context, string, string) error { return f.err }
func (f webhooksFake) RotateSecret(context.Context, string, string) (emailwebhook_usecase.WithSecret, error) {
	return emailwebhook_usecase.WithSecret{}, f.err
}
func (f webhooksFake) List(context.Context, string) ([]wh.Endpoint, error) { return nil, f.err }
func (f webhooksFake) Deliveries(context.Context, string, string, string, int) (emailwebhook_usecase.DeliveryPage, error) {
	return emailwebhook_usecase.DeliveryPage{}, f.err
}
func (f webhooksFake) Retry(context.Context, string, string, string) (wh.Delivery, error) {
	return wh.Delivery{}, f.err
}

type rig struct {
	router *mux.Router
	sender *senderFake
	log    *logFake
	perKey limiterFake
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{router: mux.NewRouter(), sender: &senderFake{}, log: &logFake{}, perKey: limiterFake{refuse: map[string]bool{}}}
	keys := keysFake{
		"send-key": {WorkspaceID: "ws-1", KeyID: "k-send", Scopes: []apikey.Scope{apikey.ScopeEmailSend}},
		"read-key": {WorkspaceID: "ws-1", KeyID: "k-read", Scopes: []apikey.Scope{apikey.ScopeEmailRead}},
		"busy-key": {WorkspaceID: "ws-1", KeyID: "k-busy", Scopes: []apikey.Scope{apikey.ScopeEmailSend}},
	}
	r.perKey.refuse["k-busy"] = true
	h := NewHandler(r.sender, r.log, webhooksFake{})
	RegisterKeyRoutes(r.router, h, apikeys.NewAuthenticator(keys, r.perKey, limiterFake{}))
	return r
}

func (r *rig) do(method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Workspace-ID", "ws-forged")
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

func TestKeyRoutesFailClosedWithCodes(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		name, method, path, token string
		status                    int
		code                      string
	}{
		{"missing", http.MethodPost, KeyRoutesPrefix, "", 401, apikeys.CodeMissing},
		{"invalid", http.MethodPost, KeyRoutesPrefix, "nope", 401, apikeys.CodeInvalid},
		{"revoked", http.MethodPost, KeyRoutesPrefix, "revoked", 401, apikeys.CodeRevoked},
		{"scope", http.MethodPost, KeyRoutesPrefix, "read-key", 403, apikeys.CodeScope},
		{"read scope", http.MethodGet, KeyRoutesPrefix, "send-key", 403, apikeys.CodeScope},
		{"unavailable", http.MethodGet, KeyRoutesPrefix, "down", 503, apikeys.CodeUnavailable},
		{"rate", http.MethodPost, KeyRoutesPrefix, "busy-key", 429, apikeys.CodeRateLimited},
	}
	for _, c := range cases {
		rec := r.do(c.method, c.path, c.token, map[string]any{})
		if rec.Code != c.status || responsetest.CodeOf(t, rec) != c.code {
			t.Fatalf("%s: %d %s, want %d %s", c.name, rec.Code, rec.Body.String(), c.status, c.code)
		}
	}
	if rec := r.do(http.MethodPost, KeyRoutesPrefix, "busy-key", map[string]any{}); rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("retry after = %q", rec.Header().Get("Retry-After"))
	}
	if r.sender.got.WorkspaceID != "" {
		t.Fatal("a refused key reached the sender")
	}
}

func TestSendActsForTheKeysWorkspaceAndIgnoresTheHeader(t *testing.T) {
	r := newRig(t)
	r.sender.result = emailapi_usecase.Result{Send: emailapi.Send{ID: "s-1", IdempotencyKey: "k"}, Messages: []emailapi.Message{{ID: "m-1", Address: "ana@x.com", Role: emailapi.RoleTo, Status: emailapi.StatusSent}}}
	rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{
		"from": "no-reply@acme.com", "to": []string{"ana@x.com"}, "subject": "Oi", "text": "x", "idempotency_key": "k",
		"tags":        []map[string]string{{"name": "kind", "value": "reset"}},
		"attachments": []map[string]string{{"filename": "a.txt", "content": base64.StdEncoding.EncodeToString([]byte("oi"))}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	got := r.sender.got
	if got.WorkspaceID != "ws-1" || got.APIKeyID != "k-send" || got.Origin != emailapi.OriginAPI || string(got.Attachments[0].Data) != "oi" || got.Tags[0].Name != "kind" {
		t.Fatalf("request = %+v", got)
	}
	var body SendResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.ID != "s-1" || body.Messages[0].Status != "sent" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	r.sender.result.Repeated = true
	if rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"idempotency_key": "k"}); rec.Code != http.StatusOK {
		t.Fatalf("repeat = %d", rec.Code)
	}
}

func TestTheIdempotencyKeyMayComeInTheHeader(t *testing.T) {
	r := newRig(t)
	req := httptest.NewRequest(http.MethodPost, KeyRoutesPrefix, strings.NewReader(`{"to":["a@b.com"]}`))
	req.Header.Set("Authorization", "Bearer send-key")
	req.Header.Set("Idempotency-Key", "from-header")
	r.router.ServeHTTP(httptest.NewRecorder(), req)
	if r.sender.got.IdempotencyKey != "from-header" {
		t.Fatalf("key = %q", r.sender.got.IdempotencyKey)
	}
}

func TestSendRefusalsCarryTheirDetails(t *testing.T) {
	r := newRig(t)
	r.sender.err = tx.Refuse(tx.CodeBalanceInsufficient, &balance_usecase.InsufficientEmailBalanceError{MissingMicros: 417})
	rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"idempotency_key": "k"})
	if rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), `"missing_micros":417`) {
		t.Fatalf("short wallet = %d %s", rec.Code, rec.Body.String())
	}
	r.sender.err = tx.SendRefusal(&es.SendError{Kind: es.SendErrorTransient, Detail: "rate", RetryAfter: 2 * time.Second})
	rec = r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"idempotency_key": "k"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" || responsetest.CodeOf(t, rec) != tx.CodeSendThrottled {
		t.Fatalf("throttle = %d %s", rec.Code, rec.Body.String())
	}
	for code, status := range map[string]int{
		emailapi_usecase.CodeIdempotencyKeyReused: 409, emailapi_usecase.CodeSenderUnknown: 422, emailapi_usecase.CodeAttachmentTooLarge: 413,
		tx.CodeRecipientSuppressed: 409, emailapi_usecase.CodeHeaderNotAllowed: 422, tx.CodeSendInProgress: 409,
	} {
		r.sender.err = tx.Refuse(code, errors.New(code))
		if rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"idempotency_key": "k"}); rec.Code != status || responsetest.CodeOf(t, rec) != code {
			t.Fatalf("%s = %d", code, rec.Code)
		}
	}
	r.sender.err = errors.New("boom")
	if rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"idempotency_key": "k"}); rec.Code != http.StatusBadGateway {
		t.Fatalf("unknown error = %d", rec.Code)
	}
	if rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"attachments": []map[string]string{{"filename": "a", "content": "%%%"}}}); responsetest.CodeOf(t, rec) != emailapi_usecase.CodeAttachmentInvalid {
		t.Fatalf("bad base64 = %s", rec.Body.String())
	}
	if rec := r.do(http.MethodPost, KeyRoutesPrefix, "send-key", map[string]any{"surprise": 1}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", rec.Code)
	}
}

func TestLogQueriesAreParsedAndScopedToTheKey(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodGet, KeyRoutesPrefix+"?status=delivered&recipient=a@b.com&tag_name=kind&tag_value=reset&after=2026-10-01T00:00:00Z&limit=10", "read-key", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	q := r.log.got
	if q.WorkspaceID != "ws-1" || q.Status != emailapi.StatusDelivered || q.Recipient != "a@b.com" || q.TagName != "kind" || q.Limit != 10 || q.From.IsZero() {
		t.Fatalf("query = %+v", q)
	}
	if rec := r.do(http.MethodGet, KeyRoutesPrefix+"?after=yesterday", "read-key", nil); responsetest.CodeOf(t, rec) != emailapi_usecase.CodeLogFilterInvalid {
		t.Fatalf("bad date = %s", rec.Body.String())
	}
	if rec := r.do(http.MethodGet, KeyRoutesPrefix+"?cursor=%25%25", "read-key", nil); responsetest.CodeOf(t, rec) != emailapi_usecase.CodeLogCursorInvalid {
		t.Fatalf("bad cursor = %s", rec.Body.String())
	}
	if rec := r.do(http.MethodGet, KeyRoutesPrefix+"/not-a-uuid", "read-key", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("bad id = %d", rec.Code)
	}
	r.do(http.MethodGet, KeyRoutesPrefix+"/0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b", "read-key", nil)
	if r.log.asked != "ws-1/0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b" {
		t.Fatalf("asked = %q", r.log.asked)
	}
}

func TestWebhookRoutesNeedUserAccess(t *testing.T) {
	router := mux.NewRouter()
	var checked []string
	ac := func(res workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		checked = append(checked, string(res)+":"+string(action))
		return next
	}
	RegisterProtectedRoutes(router, NewHandler(&senderFake{}, &logFake{}, webhooksFake{err: wh.ErrURLBlocked}), ac)
	want := map[string]bool{"email_marketing:read": true, "email_marketing:configure": true}
	for _, c := range checked {
		if !want[c] {
			t.Fatalf("unexpected guard %s", c)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/email/transactional/webhooks", strings.NewReader(`{"url":"https://10.0.0.1","events":["email.sent"]}`))
	req.Header.Set("X-Workspace-ID", "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || responsetest.CodeOf(t, rec) != "email_webhook_url_blocked" {
		t.Fatalf("blocked url = %d %s", rec.Code, rec.Body.String())
	}
}

func TestErrorCodesAreTranslated(t *testing.T) {
	codes := append(RefusalCodes(), apikeys.AuthRefusalCodes()...)
	codes = append(codes, apikeys.ManageRefusalCodes()...)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, "update testdata/error_codes.golden and translate the codes in the pt, en, es and de catalogs of vozko-front (emailMarketing.errors and apiKeys.errors)")
}

func TestRefusalsHaveStableCodesAndMessages(t *testing.T) {
	responsetest.RequireStableCodes(t, apiRefusals)
	responsetest.RequireStableCodes(t, logRefusals)
}
