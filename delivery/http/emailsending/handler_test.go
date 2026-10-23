package emailsending

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	es "vozko/domain/emailsending"
	"vozko/infra/http/middleware"
	esuc "vozko/usecases/emailsending"
)

func TestEveryDomainErrorMapsToACode(t *testing.T) {
	domainErrors := []error{
		es.ErrInvalidTenantName, es.ErrAccountNotProvisioned, es.ErrAccountPaused, es.ErrInvalidLocalPart, es.ErrInvalidDisplayName,
		es.ErrInvalidForward, es.ErrDomainNotVerified, es.ErrFreeMailboxFrom, es.ErrSenderDomainMismatch, es.ErrInvalidSlug,
		es.ErrInvalidSharedDomain, es.ErrUnknownDomainKind, es.ErrInvalidMessage, es.ErrInsecureUnsubscribe, es.ErrDailyCapReached,
		es.ErrDomainTaken, es.ErrSenderNotFound, es.ErrDomainNotFound, es.ErrAccountNotFound, es.ErrRecipientSuppressed,
		es.ErrNoDefaultSender, es.ErrRawMessageKey, es.ErrRawMessageTooLarge, es.ErrMessageNotFound, es.ErrSenderExists,
		&es.DailyCapError{Cap: 500},
		esuc.ErrDomainInvalid, esuc.ErrDomainFreeMailbox, esuc.ErrDomainShared, esuc.ErrProviderUnavailable, es.ErrUnknownConnectMethod, es.ErrUnknownSetupFailure, es.ErrUnknownReleaseState, es.ErrSlugReserved, es.ErrSlugTaken, es.ErrSubdomainLocked,
		esuc.ErrSharedDomainRemoval, esuc.ErrDomainInUse, esuc.ErrDefaultSenderRequired, esuc.ErrInvalidAddress,
		esuc.ErrSuppressionNotFound, esuc.ErrSuppressionLocked, esuc.ErrNotInProbation, es.ErrAccountReactivation, esuc.ErrReplyCapacityReached,
		es.ErrPlanRequired, es.ErrPlanUnreadable,
		fmt.Errorf("%w: identity: boom", esuc.ErrProviderUnavailable),
	}
	for _, err := range domainErrors {
		rec := httptest.NewRecorder()
		writeError(rec, fmt.Errorf("wrapped: %w", err), "test")
		var body response.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code >= 500 && rec.Code != http.StatusServiceUnavailable || body.Code == "" || body.Code == "email_request_failed" {
			t.Errorf("%v: status %d code %q", err, rec.Code, body.Code)
		}
	}
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("database exploded"), "test")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "email_request_failed") {
		t.Fatalf("unknown errors fall back to email_request_failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	writeError(rec, context.Canceled, "test")
	if rec.Body.Len() != 0 {
		t.Fatal("canceled requests write nothing")
	}
}

func TestRefusalCodesAreNamespaced(t *testing.T) {
	seen := map[string]bool{}
	for _, code := range RefusalCodes() {
		if !strings.HasPrefix(code, "email_") {
			t.Errorf("code %q is not namespaced", code)
		}
		seen[code] = true
	}
	for _, want := range []string{"email_account_not_provisioned", "email_account_paused", "email_domain_invalid", "email_domain_free_mailbox",
		"email_domain_shared", "email_domain_taken", "email_domain_not_found", "email_sender_invalid_local_part", "email_sender_invalid_display_name",
		"email_sender_invalid_forward", "email_domain_not_verified", "email_domain_releasing", "email_sender_not_found", "email_provider_unavailable", "email_reply_capacity_reached"} {
		if !seen[want] {
			t.Errorf("missing code %q", want)
		}
	}
}

type stubSetup struct {
	view  esuc.StatusView
	check esuc.SubdomainCheck
	slugs *[]string
	err   error
}

func (s stubSetup) record(slug string) {
	if s.slugs != nil {
		*s.slugs = append(*s.slugs, slug)
	}
}

func (s stubSetup) Status(context.Context, string) (esuc.StatusView, error) { return s.view, s.err }
func (s stubSetup) Provision(_ context.Context, _ string, slug string) (esuc.StatusView, error) {
	s.record("provision:" + slug)
	return s.view, s.err
}
func (s stubSetup) CheckSubdomain(_ context.Context, _ string, slug string) (esuc.SubdomainCheck, error) {
	s.record("check:" + slug)
	return s.check, s.err
}
func (s stubSetup) ChangeSubdomain(_ context.Context, _ string, slug string) (esuc.StatusView, error) {
	s.record("change:" + slug)
	return s.view, s.err
}

type stubDomains struct {
	connectArgs []string
	result      es.SendingDomain
	err         error
}

func (s *stubDomains) Get(context.Context, string, string) (*es.SendingDomain, error) {
	return &s.result, s.err
}

func (s *stubDomains) Connect(_ context.Context, ws, domain string) (*es.SendingDomain, error) {
	s.connectArgs = []string{ws, domain}
	return &s.result, s.err
}

func (s *stubDomains) Recheck(context.Context, string, string) (*es.SendingDomain, error) {
	return &s.result, s.err
}

func (s *stubDomains) Remove(context.Context, string, string) error { return s.err }

type stubSenders struct{}

func (stubSenders) List(context.Context, string) ([]esuc.SenderView, error) { return nil, nil }
func (stubSenders) Create(context.Context, string, esuc.CreateSenderInput) (esuc.SenderView, error) {
	return esuc.SenderView{}, es.ErrInvalidLocalPart
}
func (stubSenders) Update(context.Context, string, string, esuc.UpdateSenderInput) (esuc.SenderView, error) {
	return esuc.SenderView{}, nil
}
func (stubSenders) Delete(context.Context, string, string) error { return nil }

type stubSuppressions struct {
	removed string
	search  string
}

func (s *stubSuppressions) List(_ context.Context, _ string, search string, _, _ int) ([]es.Suppression, int64, error) {
	s.search = search
	return []es.Suppression{{Address: "a@b.com", Reason: es.SuppressManual}}, 1, nil
}
func (s *stubSuppressions) Add(context.Context, string, string) (es.Suppression, error) {
	return es.Suppression{}, nil
}
func (s *stubSuppressions) Remove(_ context.Context, _ string, address string) error {
	s.removed = address
	return esuc.ErrSuppressionLocked
}

func router(t *testing.T, h *Handler, calls *[]string) *mux.Router {
	t.Helper()
	r := mux.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")))
		})
	})
	guard := func(name string) Guard {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, req *http.Request) {
				*calls = append(*calls, name)
				next(w, req)
			}
		}
	}
	RegisterProtectedRoutes(r, h, Guards{View: guard("view"), Manage: guard("manage")})
	return r
}

func TestHandlersAndGuards(t *testing.T) {
	domains := &stubDomains{result: es.SendingDomain{
		ID: "dom-1", Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainVerifying, ConnectMethod: es.ConnectManual,
		Records: []es.DNSRecord{{Purpose: es.RecordDKIM, Type: "CNAME", Name: "k._domainkey.loja.com.br", Value: "k.dkim"}},
	}}
	suppressions := &stubSuppressions{}
	setup := stubSetup{view: esuc.StatusView{Provisioned: true, Account: &es.SenderAccount{ID: "acct", Mode: es.ModeProbation, Status: es.AccountActive},
		Domains: []esuc.DomainView{{Domain: es.SendingDomain{ID: "d", Domain: "x.envios.test"}, UsedToday: 3, CapToday: 500}},
		Senders: []esuc.SenderView{{Sender: es.Sender{ID: "s", DomainID: "d", LocalPart: "contato"}, Address: "contato@x.envios.test", Domain: es.SendingDomain{ID: "d", Domain: "x.envios.test"}}}}}
	h := NewHandler(setup, domains, stubSenders{}, suppressions)
	var calls []string
	r := router(t, h, &calls)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/email/setup", nil))
	var setupBody SetupResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &setupBody)
	if rec.Code != http.StatusOK || !setupBody.Provisioned || setupBody.Senders[0].Address != "contato@x.envios.test" || *setupBody.Domains[0].CapToday != 500 {
		t.Fatalf("setup = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/domains", strings.NewReader(`{"domain":"loja.com.br"}`)))
	var connected DomainResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &connected)
	if rec.Code != http.StatusCreated || connected.ID != "dom-1" || connected.ConnectMethod != "manual" || len(connected.Records) != 1 || connected.Records[0].Host != "k._domainkey" {
		t.Fatalf("connect = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"session"`) {
		t.Fatalf("connecting returns the domain itself: %s", rec.Body.String())
	}
	if strings.Join(domains.connectArgs, ",") != "ws-1,loja.com.br" {
		t.Fatalf("args = %v", domains.connectArgs)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/domains", strings.NewReader(`not json`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/senders", strings.NewReader(`{"local_part":"postmaster"}`)))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "email_sender_invalid_local_part") {
		t.Fatalf("sender refusal = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/email/suppressions/queixa+1@example.com", nil))
	if rec.Code != http.StatusConflict || suppressions.removed != "queixa+1@example.com" {
		t.Fatalf("remove = %d %q", rec.Code, suppressions.removed)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/email/suppressions?page=1&pageSize=10&q=Ana%2B1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"totalItems":1`) || suppressions.search != "Ana+1" {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}

	want := "view,manage,manage,manage,manage,view"
	if strings.Join(calls, ",") != want {
		t.Fatalf("guards = %v, want %s", calls, want)
	}
}

func TestRoutesRefuseMissingGuards(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	RegisterProtectedRoutes(mux.NewRouter(), NewHandler(stubSetup{}, &stubDomains{}, stubSenders{}, &stubSuppressions{}), Guards{})
}

type stubAdmin struct{ err error }

func (s stubAdmin) List(context.Context, int, int) ([]esuc.AccountHealthView, int64, error) {
	return []esuc.AccountHealthView{{Account: es.SenderAccount{ID: "a"}, WorkspaceName: "Loja da Joana", Health: &es.TenantHealth{Sending: es.TenantEnabled}}}, 1, s.err
}
func (s stubAdmin) Pause(_ context.Context, ws, reason string) (*es.SenderAccount, error) {
	return &es.SenderAccount{WorkspaceID: ws, StatusReason: reason, Status: es.AccountPausedByPlatform}, s.err
}
func (s stubAdmin) Reinstate(_ context.Context, ws string) (*es.SenderAccount, error) {
	return &es.SenderAccount{WorkspaceID: ws}, s.err
}
func (s stubAdmin) Promote(_ context.Context, ws string) (*es.SenderAccount, error) {
	return &es.SenderAccount{WorkspaceID: ws}, s.err
}
func (s stubAdmin) SendQuota(context.Context) (esuc.SendQuotaView, error) {
	readAt := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	quota := es.SendQuota{MaxSendRate: 14, Max24HourSend: 50000, SentLast24Hours: 39990, ProductionAccess: true, Read: true, ReadAt: readAt}
	return esuc.SendQuotaView{Quota: quota, SentSinceRead: 10, Used24Hours: 40000, UsageBasisPoints: 8000, PriorityReserve: 2500, Fresh: true}, s.err
}

func TestAdminRoutes(t *testing.T) {
	r := mux.NewRouter()
	RegisterAdminRoutes(r, NewAdminHandler(stubAdmin{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/email/accounts/ws-9/pause", strings.NewReader(`{"reason":"abuse"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status_reason":"abuse"`) || !strings.Contains(rec.Body.String(), `"workspace_id":"ws-9"`) {
		t.Fatalf("pause = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/email/accounts", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sending":"enabled"`) || !strings.Contains(rec.Body.String(), `"workspace_name":"Loja da Joana"`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/email/quota", nil))
	for _, want := range []string{`"max_send_rate":14`, `"max_24_hour_send":50000`, `"used_24_hours":40000`, `"usage_basis_points":8000`, `"priority_reserve":2500`, `"near_limit":true`, `"production_access":true`, `"read":true`, `"fresh":true`, `"read_at":"2026-10-21T12:00:00Z"`} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("quota = %d %s, want %s", rec.Code, rec.Body.String(), want)
		}
	}
	r = mux.NewRouter()
	RegisterAdminRoutes(r, NewAdminHandler(stubAdmin{err: esuc.ErrNotInProbation}))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/email/accounts/ws-9/promote", nil))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "email_account_not_in_probation") {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body.String())
	}
}

func TestDomainRecordsCarryHostAndStatus(t *testing.T) {
	d := es.SendingDomain{ID: "d", Domain: "loja.com.br", Records: []es.DNSRecord{
		{Purpose: es.RecordDKIM, Type: "CNAME", Name: "k._domainkey.loja.com.br", Value: "k.dkim", Status: es.RecordVerified},
		{Purpose: es.RecordReplyMX, Type: "MX", Name: "reply.loja.com.br", Value: "inbound", Priority: 10, Status: es.RecordUnknown},
		{Purpose: es.RecordDMARC, Type: "TXT", Name: "_dmarc.loja.com.br", Value: "v=DMARC1; p=none;"},
		{Purpose: es.RecordMailFromMX, Type: "MX", Name: "loja.com.br", Value: "feedback"},
	}}
	got := presentDomain(d).Records
	want := []struct{ host, status string }{
		{"k._domainkey", "verified"}, {"reply", "unknown"}, {"_dmarc", "pending"}, {"@", "pending"},
	}
	for i, w := range want {
		if got[i].Host != w.host || got[i].Status != w.status || got[i].Name != d.Records[i].Name {
			t.Errorf("record %d = %+v, want host %q status %q", i, got[i], w.host, w.status)
		}
	}
}

func TestProbationCarriesTheTargets(t *testing.T) {
	view := esuc.StatusView{Provisioned: true, Probation: &esuc.ProbationView{Days: 3, RequiredDays: 14, Targets: es.CurrentPromotionTargets()}}
	got := presentSetup(view).Probation.Targets
	if got.Days != 14 || got.MinDelivered != 200 || got.MaxBouncePerMil != 20 || got.MaxComplaintPer10k != 8 {
		t.Fatalf("targets = %+v", got)
	}
}

func TestAFailedReactivationIsCodedAsUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, fmt.Errorf("%w: %v", es.ErrAccountReactivation, fmt.Errorf("%w: revive tenant: boom", esuc.ErrProviderUnavailable)), "test")
	var body response.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "email_account_reactivation_failed" {
		t.Fatalf("status %d code %q", rec.Code, body.Code)
	}
}

func TestAdminListShowsADormantAccountWithoutHealth(t *testing.T) {
	got := presentAdminAccount(esuc.AccountHealthView{Account: es.SenderAccount{ID: "acct", Status: es.AccountDormant}, WorkspaceName: "Loja"})
	raw, _ := json.Marshal(got)
	body := string(raw)
	for _, want := range []string{`"status":"dormant"`, `"sending":""`, `"health_error":""`, `"findings":[]`, `"health_checked_at":null`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
}

func TestSetupCarriesTheSetupProgressWithBackendCodes(t *testing.T) {
	failed := es.SendingDomain{ID: "d1", Domain: "loja.envios.test", Status: es.DomainPending, SetupFailure: es.SetupFailureReplyCapacity, FailureReason: "inbound: full"}
	view := esuc.StatusView{
		Provisioned: true,
		Domains:     []esuc.DomainView{{Domain: failed}},
		Setup:       es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureReplyCapacity},
	}
	raw, _ := json.Marshal(presentSetup(view))
	body := string(raw)
	want := `"setup":{"state":"failed","error_code":"email_reply_capacity_reached"}`
	if strings.Count(body, want) != 2 {
		t.Fatalf("the account and the domain must both carry %s: %s", want, body)
	}

	inProgress := presentDomain(es.SendingDomain{ID: "d2", Status: es.DomainPending})
	raw, _ = json.Marshal(inProgress)
	if !strings.Contains(string(raw), `"setup":{"state":"in_progress"}`) {
		t.Fatalf("domain = %s", raw)
	}
	provider := presentDomain(es.SendingDomain{ID: "d3", Status: es.DomainPending, SetupFailure: es.SetupFailureProvider})
	if provider.Setup.ErrorCode != "email_provider_unavailable" {
		t.Fatalf("setup = %+v", provider.Setup)
	}
	done := presentDomain(es.SendingDomain{ID: "d4", Status: es.DomainVerified})
	if done.Setup != (SetupProgressResponse{State: "done"}) {
		t.Fatalf("setup = %+v", done.Setup)
	}
}

func TestEverySetupFailureHasARefusalCode(t *testing.T) {
	codes := map[string]bool{}
	for _, code := range RefusalCodes() {
		codes[code] = true
	}
	for _, failure := range []es.SetupFailure{es.SetupFailureProvider, es.SetupFailureReplyCapacity} {
		code := setupErrorCode(failure)
		if code == "" || !codes[code] {
			t.Errorf("setup failure %q maps to %q, not a refusal code", failure, code)
		}
	}
}

func TestSubdomainRoutes(t *testing.T) {
	var slugs, calls []string
	setup := stubSetup{
		view:  esuc.StatusView{Provisioned: true, Subdomain: esuc.SubdomainView{Slug: "cartaodetodososasco", Address: "contato@cartaodetodososasco.vozkomsg.com.br"}},
		check: esuc.SubdomainCheck{Slug: "outra", Address: "contato@outra.vozkomsg.com.br", Refusal: es.ErrSlugTaken},
		slugs: &slugs,
	}
	r := router(t, NewHandler(setup, &stubDomains{}, stubSenders{}, &stubSuppressions{}), &calls)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/email/setup/subdomain/check?slug=outra", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) || !strings.Contains(rec.Body.String(), `"error_code":"email_subdomain_taken"`) ||
		!strings.Contains(rec.Body.String(), `"address":"contato@outra.vozkomsg.com.br"`) {
		t.Fatalf("check = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/setup/provision", strings.NewReader(`{"slug":"cartaodetodososasco"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subdomain":{"slug":"cartaodetodososasco","address":"contato@cartaodetodososasco.vozkomsg.com.br","locked":false}`) {
		t.Fatalf("provision = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/email/setup/provision", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("provision without a body = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/email/setup/subdomain", strings.NewReader(`{"slug":"nova"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("change = %d %s", rec.Code, rec.Body.String())
	}

	if strings.Join(slugs, ",") != "check:outra,provision:cartaodetodososasco,provision:,change:nova" {
		t.Fatalf("slugs = %v", slugs)
	}
	if strings.Join(calls, ",") != "manage,manage,manage,manage" {
		t.Fatalf("guards = %v", calls)
	}
}

func TestSubdomainRefusalsAreCoded(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		es.ErrInvalidSlug:     {http.StatusUnprocessableEntity, "email_subdomain_invalid"},
		es.ErrSlugReserved:    {http.StatusUnprocessableEntity, "email_subdomain_reserved"},
		es.ErrSlugTaken:       {http.StatusConflict, "email_subdomain_taken"},
		es.ErrSubdomainLocked: {http.StatusConflict, "email_subdomain_locked"},
	}
	for err, want := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, err, "test")
		var body response.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != want.status || body.Code != want.code {
			t.Errorf("%v = %d %q, want %d %q", err, rec.Code, body.Code, want.status, want.code)
		}
	}
	available := presentSubdomainCheck(esuc.SubdomainCheck{Slug: "nova", Address: "contato@nova.vozkomsg.com.br"})
	if !available.Available || available.ErrorCode != "" {
		t.Fatalf("available = %+v", available)
	}
	invalid := presentSubdomainCheck(esuc.SubdomainCheck{Slug: "Nova", Refusal: es.ErrInvalidSlug})
	if invalid.Available || invalid.ErrorCode != "email_subdomain_invalid" || invalid.Address != "" {
		t.Fatalf("invalid = %+v", invalid)
	}
}

func TestSuppressionRowsSayWhetherTheyCanBeRemoved(t *testing.T) {
	cases := map[es.SuppressReason]bool{es.SuppressHardBounce: false, es.SuppressComplaint: false, es.SuppressManual: true, es.SuppressSoftBounce: true}
	for reason, want := range cases {
		if got := presentSuppression(es.Suppression{Address: "a@b.com", Reason: reason}).Removable; got != want {
			t.Fatalf("%s removable = %v, want %v", reason, got, want)
		}
	}
}
