package transactionalemail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/emaildesign"
	es "vozko/domain/emailsending"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
)

type harness struct {
	log        *recorder
	gateway    *gatewayFake
	billing    *billingFake
	runs       *runsFake
	designs    designsFake
	facts      factsFake
	leads      leadsFake
	transcript *transcriptFake
}

func newHarness() *harness {
	log := &recorder{}
	return &harness{
		log:     log,
		gateway: &gatewayFake{log: log},
		billing: &billingFake{log: log, quote: balance_usecase.EmailQuote{Units: 1, UnitMicros: 583, TotalMicros: 583}},
		runs:    &runsFake{},
		designs: designsFake{designs: map[string]emaildesign.Document{}},
		facts: factsFake{facts: map[string]lead.EmailFacts{
			"lead-1":  {ID: "lead-1", Name: "Ana Souza", Email: "Ana@Acme.com.br", Company: "Acme", CustomFields: map[string]any{"plano": "ouro"}},
			"blocked": {ID: "blocked", Name: "Bia", Email: "bia@acme.com.br", Blocked: true},
			"opted":   {ID: "opted", Name: "Caio", Email: "caio@acme.com.br", OptedOut: true},
		}},
		leads:      leadsFake{byEmail: map[string]string{"bia@acme.com.br": "blocked", "ana@acme.com.br": "lead-1"}},
		transcript: &transcriptFake{log: log},
	}
}

func (h *harness) service(t *testing.T) *Service {
	t.Helper()
	s, err := New(Deps{Gateway: h.gateway, Billing: h.billing, Runs: h.runs, Designs: h.designs, LeadFacts: h.facts, Leads: h.leads, Transcript: h.transcript})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func textRequest() Request {
	return Request{
		WorkspaceID:    "ws-1",
		LeadID:         "lead-1",
		Subject:        "Pedido de {{first_name}}",
		Body:           Body{Text: "Oi {{first_name}},\n\nplano {{field.plano}} & pedido {{field.pedido}}."},
		Variables:      map[string]string{"pedido": "A-17"},
		IdempotencyKey: "workflow:run-1:node-1:node-1#3",
		Origin:         OriginWorkflow,
		SentBy:         conversation.SentByWorkflow("wf-1"),
	}
}

func runIDOf(workspaceID, key string) string {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var refusal *Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("err %v is not a refusal", err)
	}
	return refusal.Code
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	h := newHarness()
	full := Deps{Gateway: h.gateway, Billing: h.billing, Runs: h.runs, Designs: h.designs, LeadFacts: h.facts, Leads: h.leads, Transcript: h.transcript}
	drops := []func(*Deps){
		func(d *Deps) { d.Gateway = nil },
		func(d *Deps) { d.Billing = nil },
		func(d *Deps) { d.Runs = nil },
		func(d *Deps) { d.Designs = nil },
		func(d *Deps) { d.LeadFacts = nil },
		func(d *Deps) { d.Leads = nil },
		func(d *Deps) { d.Transcript = nil },
	}
	for i, drop := range drops {
		deps := full
		drop(&deps)
		if _, err := New(deps); !errors.Is(err, ErrIncomplete) {
			t.Errorf("drop %d: err = %v", i, err)
		}
	}
}

func TestSendChargesThenSendsATransactionalMessage(t *testing.T) {
	h := newHarness()
	res, err := h.service(t).Send(context.Background(), textRequest())
	if err != nil {
		t.Fatal(err)
	}
	if want := []event{"charge", "send", "transcribe"}; !reflect.DeepEqual(h.log.events, want) {
		t.Fatalf("events = %v, want %v", h.log.events, want)
	}
	runID := runIDOf("ws-1", textRequest().IdempotencyKey)
	if h.billing.charged[0] != "tx:"+runID {
		t.Fatalf("charge key = %q", h.billing.charged[0])
	}
	req := h.gateway.requests[0]
	if req.Kind != es.KindTransactional || req.RunKind != es.RunTriggered || req.RunID != runID || req.To != "ana@acme.com.br" || req.SenderID != "" || req.WorkspaceID != "ws-1" || req.Tags["origin"] != "workflow" {
		t.Fatalf("request = %+v", req)
	}
	content := h.gateway.contents[0]
	if content.Subject != "Pedido de Ana" {
		t.Fatalf("subject = %q", content.Subject)
	}
	if content.Text != "Oi Ana,\n\nplano ouro & pedido A-17." {
		t.Fatalf("text = %q", content.Text)
	}
	if !strings.Contains(content.HTML, "plano ouro &amp; pedido A-17.") || !strings.Contains(content.HTML, "Oi Ana,") {
		t.Fatalf("html = %q", content.HTML)
	}
	if res.MessageID != "msg-1" || res.ProviderMessageID != "prov-1" || res.To != "ana@acme.com.br" || res.Repeated {
		t.Fatalf("result = %+v", res)
	}
	if h.runs.asked[0] != "ws-1|triggered|"+runID {
		t.Fatalf("idempotency lookup = %v", h.runs.asked)
	}
}

func TestSendPassesTheChosenSenderAndAnExplicitRecipient(t *testing.T) {
	h := newHarness()
	req := textRequest()
	req.SenderID = "sender-9"
	req.To = "  Outro@Exemplo.com "
	if _, err := h.service(t).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got := h.gateway.requests[0]
	if got.SenderID != "sender-9" || got.To != "outro@exemplo.com" {
		t.Fatalf("request = %+v", got)
	}
}

func TestSendRefundsEveryFailedSendWithACode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"suppressed", es.ErrRecipientSuppressed, CodeRecipientSuppressed},
		{"no default sender", es.ErrNoDefaultSender, CodeSenderMissing},
		{"sender gone", es.ErrSenderNotFound, CodeSenderMissing},
		{"not provisioned", es.ErrAccountNotProvisioned, CodeSenderMissing},
		{"unverified domain", es.ErrDomainNotVerified, CodeSenderNotVerified},
		{"free mailbox", es.ErrFreeMailboxFrom, CodeSenderNotVerified},
		{"paused account", es.ErrAccountPaused, CodeAccountPaused},
		{"dormant account that did not come back", es.ErrAccountReactivation, CodeAccountReactivation},
		{"provider paused", &es.SendError{Kind: es.SendErrorPaused}, CodeAccountPaused},
		{"daily cap", &es.DailyCapError{Cap: 50, ResumeAt: time.Now()}, CodeDailyCapReached},
		{"invalid message", es.ErrInvalidMessage, CodeMessageInvalid},
		{"provider down", errors.New("timeout"), CodeSendFailed},
	}
	for _, tc := range cases {
		h := newHarness()
		h.gateway.err = tc.err
		_, err := h.service(t).Send(context.Background(), textRequest())
		if code := codeOf(t, err); code != tc.code {
			t.Errorf("%s: code = %s, want %s", tc.name, code, tc.code)
		}
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: cause lost: %v", tc.name, err)
		}
		if want := []event{"charge", "send", "refund"}; !reflect.DeepEqual(h.log.events, want) {
			t.Errorf("%s: events = %v", tc.name, h.log.events)
		}
		if len(h.billing.refunded) != 1 || h.billing.refunded[0] != h.billing.charged[0]+"#round" {
			t.Errorf("%s: refund must use the charged round key: %v", tc.name, h.billing.refunded)
		}
	}
}

func TestSendStillRefusesWhenTheRefundFails(t *testing.T) {
	h := newHarness()
	h.gateway.err = es.ErrRecipientSuppressed
	h.billing.refundErr = errors.New("ledger down")
	_, err := h.service(t).Send(context.Background(), textRequest())
	if codeOf(t, err) != CodeRecipientSuppressed {
		t.Fatalf("err = %v", err)
	}
}

func TestSendNeverSendsUnpaid(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"short wallet", &balance_usecase.InsufficientEmailBalanceError{MissingMicros: 10}, CodeBalanceInsufficient},
		{"no price", balance.ErrPriceUnavailable, CodeChargeFailed},
		{"ledger down", errors.New("db down"), CodeChargeFailed},
	}
	for _, tc := range cases {
		h := newHarness()
		h.billing.chargeErr = tc.err
		_, err := h.service(t).Send(context.Background(), textRequest())
		if codeOf(t, err) != tc.code || !errors.Is(err, tc.err) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if len(h.gateway.requests) != 0 || len(h.billing.refunded) != 0 {
			t.Errorf("%s: sent or refunded without a charge", tc.name)
		}
	}
}

func TestSendRepeatedKeyReturnsTheRecordedMessage(t *testing.T) {
	statuses := []es.MessageStatus{es.MessageQueued, es.MessageSent, es.MessageDelivered, es.MessageBounced, es.MessageSuppressed}
	for _, status := range statuses {
		h := newHarness()
		h.runs.rows = []es.Message{
			{ID: "old-failed", Status: es.MessageFailed},
			{ID: "kept", ProviderMessageID: "prov-kept", Status: status, Address: "ana@acme.com.br"},
		}
		res, err := h.service(t).Send(context.Background(), textRequest())
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if !res.Repeated || res.MessageID != "kept" || res.ProviderMessageID != "prov-kept" || res.To != "ana@acme.com.br" {
			t.Fatalf("%s: result = %+v", status, res)
		}
		if want := []event{"transcribe"}; !reflect.DeepEqual(h.log.events, want) {
			t.Fatalf("%s: a repeat must neither charge nor send: %v", status, h.log.events)
		}
	}
}

func TestSendRetriesAfterOnlyFailedAttempts(t *testing.T) {
	h := newHarness()
	h.runs.rows = []es.Message{{ID: "f1", Status: es.MessageFailed}, {ID: "f2", Status: es.MessageFailed}}
	res, err := h.service(t).Send(context.Background(), textRequest())
	if err != nil || res.Repeated || res.MessageID != "msg-1" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if want := []event{"charge", "send", "transcribe"}; !reflect.DeepEqual(h.log.events, want) {
		t.Fatalf("events = %v", h.log.events)
	}
}

func TestSendFailsClosedWhenTheIdempotencyLookupFails(t *testing.T) {
	h := newHarness()
	h.runs.err = errors.New("db down")
	if _, err := h.service(t).Send(context.Background(), textRequest()); err == nil || len(h.log.events) != 0 {
		t.Fatalf("err = %v, events = %v", err, h.log.events)
	}
}

func TestSendRefusesBeforeChargingWithACode(t *testing.T) {
	h := newHarness()
	h.designs.designs["ws-1/d-1"] = emaildesign.Document{Version: 99}
	cases := []struct {
		name   string
		mutate func(*Request)
		code   string
	}{
		{"blocked lead", func(r *Request) { r.LeadID = "blocked"; r.To = "" }, CodeLeadBlocked},
		{"blocked lead found by address", func(r *Request) { r.LeadID = ""; r.To = "BIA@acme.com.br" }, CodeLeadBlocked},
		{"unknown lead", func(r *Request) { r.LeadID = "ghost" }, CodeLeadNotFound},
		{"no recipient", func(r *Request) { r.LeadID = ""; r.To = " " }, CodeRecipientMissing},
		{"bad address", func(r *Request) { r.To = "not-an-address" }, CodeAddressInvalid},
		{"no body", func(r *Request) { r.Body = Body{} }, CodeMessageInvalid},
		{"two bodies", func(r *Request) { r.Body = Body{Text: "Oi", DesignID: "d-1"} }, CodeMessageInvalid},
		{"empty text", func(r *Request) { r.Body = Body{Text: "  \n"} }, CodeMessageInvalid},
		{"unknown merge tag", func(r *Request) { r.Body = Body{Text: "Oi {{nome}}"} }, CodeMessageInvalid},
		{"missing design", func(r *Request) { r.Body = Body{DesignID: "nope"} }, CodeDesignNotFound},
		{"broken design", func(r *Request) { r.Body = Body{DesignID: "d-1"} }, CodeMessageInvalid},
		{"empty subject", func(r *Request) { r.Subject = " " }, CodeSubjectInvalid},
		{"subject merge tag", func(r *Request) { r.Subject = "Oi {{nome}}" }, CodeSubjectInvalid},
	}
	for _, tc := range cases {
		h.log.events, h.gateway.requests = nil, nil
		req := textRequest()
		tc.mutate(&req)
		_, err := h.service(t).Send(context.Background(), req)
		if code := codeOf(t, err); code != tc.code {
			t.Errorf("%s: code = %s (%v), want %s", tc.name, code, err, tc.code)
		}
		if len(h.log.events) != 0 {
			t.Errorf("%s: refused requests must not charge or send: %v", tc.name, h.log.events)
		}
	}
}

func TestSendRejectsProgrammingErrors(t *testing.T) {
	h := newHarness()
	cases := []struct {
		name   string
		mutate func(*Request)
		want   error
	}{
		{"no workspace", func(r *Request) { r.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"no idempotency key", func(r *Request) { r.IdempotencyKey = " " }, ErrIdempotencyKeyRequired},
		{"unknown origin", func(r *Request) { r.Origin = "fax" }, ErrOriginInvalid},
	}
	for _, tc := range cases {
		req := textRequest()
		tc.mutate(&req)
		if _, err := h.service(t).Send(context.Background(), req); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	if len(h.log.events) != 0 {
		t.Fatalf("events = %v", h.log.events)
	}
}

func TestSendDoesNotNeedMarketingConsent(t *testing.T) {
	h := newHarness()
	req := textRequest()
	req.LeadID = "opted"
	req.To = ""
	req.Body = Body{Text: "Seu código"}
	res, err := h.service(t).Send(context.Background(), req)
	if err != nil || res.To != "caio@acme.com.br" {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestSendFindsTheLeadByAddressForPersonalization(t *testing.T) {
	h := newHarness()
	req := textRequest()
	req.LeadID = ""
	req.To = "ana@acme.com.br"
	if _, err := h.service(t).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if h.gateway.contents[0].Subject != "Pedido de Ana" {
		t.Fatalf("subject = %q", h.gateway.contents[0].Subject)
	}
	h2 := newHarness()
	req.To = "ninguem@exemplo.com"
	req.Subject = "Oi {{first_name|cliente}}"
	if _, err := h2.service(t).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if h2.gateway.contents[0].Subject != "Oi cliente" {
		t.Fatalf("subject = %q", h2.gateway.contents[0].Subject)
	}
	h3 := newHarness()
	h3.leads.err = errors.New("db down")
	if _, err := h3.service(t).Send(context.Background(), req); err == nil || len(h3.log.events) != 0 {
		t.Fatalf("an unreadable lead must fail closed: %v, %v", err, h3.log.events)
	}
}

func TestSendRendersDesignsAsTransactional(t *testing.T) {
	doc := emaildesign.Document{Version: emaildesign.CurrentVersion, Blocks: []emaildesign.Block{
		{ID: "h1", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Oi {{first_name}}, pedido {{field.pedido}}", Level: 1}},
	}}
	h := newHarness()
	h.designs.designs["ws-1/d-1"] = doc
	req := textRequest()
	req.Body = Body{DesignID: "d-1"}
	if _, err := h.service(t).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	content := h.gateway.contents[0]
	if !strings.Contains(content.HTML, "Oi Ana, pedido A-17") || !strings.Contains(content.Text, "Oi Ana, pedido A-17") || strings.Contains(content.HTML, "Descadastrar") {
		t.Fatalf("content = %+v", content)
	}

	inline := newHarness()
	req.Body = Body{Document: &doc}
	if _, err := inline.service(t).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inline.gateway.contents[0].HTML, "Oi Ana, pedido A-17") {
		t.Fatalf("inline html = %q", inline.gateway.contents[0].HTML)
	}
}

func TestRelayFailsClosedUntilBound(t *testing.T) {
	relay := NewRelay()
	_, err := relay.Send(context.Background(), textRequest())
	if codeOf(t, err) != CodeUnavailable || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbound relay = %v", err)
	}
	h := newHarness()
	relay.Bind(h.service(t))
	if res, err := relay.Send(context.Background(), textRequest()); err != nil || res.MessageID != "msg-1" {
		t.Fatalf("bound relay = %+v, %v", res, err)
	}
}

func TestCodeOf(t *testing.T) {
	if CodeOf(&Refusal{Code: CodeLeadBlocked, Err: errors.New("x")}) != CodeLeadBlocked {
		t.Fatal("refusal code")
	}
	if CodeOf(errors.New("boom")) != CodeSendFailed {
		t.Fatal("unknown errors read as send failures")
	}
	if CodeOf(ErrIdempotencyKeyRequired) != CodeRequestInvalid {
		t.Fatal("programming errors read as invalid requests")
	}
}
