package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/cmcom"
)

type backendCall struct {
	path    string
	headers http.Header
	body    []byte
}

type backend struct {
	mu     sync.Mutex
	calls  []backendCall
	server *httptest.Server
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.calls = append(b.calls, backendCall{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		b.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(b.server.Close)
	return b
}

func (b *backend) base() string {
	return b.server.URL + "/webhooks/carrier/cmcom/tok"
}

func (b *backend) callsTo(kind string) []backendCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []backendCall
	for _, c := range b.calls {
		if strings.HasSuffix(c.path, "/"+kind) {
			out = append(out, c)
		}
	}
	return out
}

type scheduled struct {
	after time.Duration
	run   func()
}

type manualClock struct {
	mu      sync.Mutex
	pending []scheduled
}

func (c *manualClock) after(d time.Duration, run func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, scheduled{after: d, run: run})
}

func (c *manualClock) runAll() []time.Duration {
	c.mu.Lock()
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	var delays []time.Duration
	for _, s := range pending {
		delays = append(delays, s.after)
		s.run()
	}
	return delays
}

type harness struct {
	backend *backend
	clock   *manualClock
	server  *httptest.Server
	client  *cmcom.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	b := newBackend(t)
	clock := &manualClock{}
	a, err := newApp(config{addr: defaultAddr, productToken: testToken, accountID: testAccount, webhookBase: b.base()}, clock.after)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	server := httptest.NewServer(a)
	t.Cleanup(server.Close)
	client, err := cmcom.NewClient(cmcom.Config{ProductToken: testToken, AccountID: testAccount, GatewayURL: server.URL, APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{backend: b, clock: clock, server: server, client: client}
}

func reference(n int) string {
	return fmt.Sprintf("%032d", n)
}

func (h *harness) sendRCS(t *testing.T, n int, phone string) carriermessaging.SendOutcome {
	t.Helper()
	content := rcs.Content{Text: "oi", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Quero"}}}.WithTrackedPostbacks()
	out, err := h.client.Send(context.Background(), carriermessaging.SendRequest{
		Reference: reference(n), SenderProfileID: "agent_1", Phone: phone, Content: content, Validity: time.Hour,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	return out
}

func (h *harness) sendSMS(t *testing.T, n int, phone string) carriermessaging.SendOutcome {
	t.Helper()
	out, err := h.client.SendSMS(context.Background(), carriermessaging.SMSRequest{Reference: reference(n), From: "29292", Phone: phone, Text: "Loja: oi"})
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	return out
}

func (h *harness) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(h.server.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func statusesByReference(t *testing.T, calls []backendCall) map[string][]carriermessaging.StatusReport {
	t.Helper()
	out := map[string][]carriermessaging.StatusReport{}
	for _, c := range calls {
		reports, err := cmcom.DecodeStatusReports(c.headers.Get("Content-Type"), c.body)
		if err != nil {
			t.Fatalf("status report does not decode: %v\n%s", err, c.body)
		}
		for _, r := range reports {
			out[r.Reference] = append(out[r.Reference], r)
		}
	}
	return out
}

func TestSendsPostTheScriptedLifecycle(t *testing.T) {
	h := newHarness(t)
	if out := h.sendRCS(t, 1, "5511987654320"); !out.Accepted {
		t.Fatalf("ending 0 = %+v", out)
	}
	if out := h.sendRCS(t, 2, "5511987654321"); !out.Accepted {
		t.Fatalf("ending 1 = %+v", out)
	}
	if out := h.sendRCS(t, 3, "5511987654322"); !out.Accepted {
		t.Fatalf("ending 2 = %+v", out)
	}
	if out := h.sendRCS(t, 4, "5511987654323"); out.Accepted || out.ErrorCode == "" {
		t.Fatalf("ending 3 must be rejected at once, got %+v", out)
	}
	if out := h.sendSMS(t, 5, "5511987654321"); !out.Accepted {
		t.Fatalf("sms = %+v", out)
	}
	if len(h.backend.callsTo("status")) != 0 {
		t.Fatalf("reports must wait for their delay")
	}
	delays := h.clock.runAll()
	if len(delays) != 7 {
		t.Fatalf("scheduled %d reports, want 7: %v", len(delays), delays)
	}
	calls := h.backend.callsTo("status")
	for _, c := range calls {
		if c.headers.Get("CF-Connecting-IP") != sourceIP || c.headers.Get("X-Forwarded-For") != sourceIP {
			t.Fatalf("report headers = %v", c.headers)
		}
		if c.path != "/webhooks/carrier/cmcom/tok/status" {
			t.Fatalf("report path = %s", c.path)
		}
	}
	got := statusesByReference(t, calls)
	want := map[string][]carriermessaging.Status{
		reference(1): {carriermessaging.StatusAccepted, carriermessaging.StatusDelivered, carriermessaging.StatusRead},
		reference(2): {carriermessaging.StatusAccepted, carriermessaging.StatusFailed},
		reference(5): {carriermessaging.StatusAccepted, carriermessaging.StatusDelivered},
	}
	if len(got) != len(want) {
		t.Fatalf("reports for %d references, want %d: %+v", len(got), len(want), got)
	}
	for ref, statuses := range want {
		if len(got[ref]) != len(statuses) {
			t.Fatalf("%s: %+v", ref, got[ref])
		}
		for i, s := range statuses {
			if got[ref][i].Status != s {
				t.Fatalf("%s report %d = %s, want %s", ref, i, got[ref][i].Status, s)
			}
		}
	}
	if failed := got[reference(2)][1]; !failed.Unreachable || failed.ErrorCode != "42" {
		t.Fatalf("no rcs report = %+v", failed)
	}
}

func TestSendsListing(t *testing.T) {
	h := newHarness(t)
	h.sendRCS(t, 1, "5511987654320")
	h.sendRCS(t, 2, "5511987654323")
	h.clock.runAll()
	res, err := http.Get(h.server.URL + "/_fake/sends")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sends []sendRecord
	if err := json.NewDecoder(res.Body).Decode(&sends); err != nil {
		t.Fatal(err)
	}
	if len(sends) != 2 {
		t.Fatalf("sends = %+v", sends)
	}
	first, second := sends[0], sends[1]
	if first.Reference != reference(1) || first.Channel != channelRCS || first.From != "agent_1" || first.Phone != "5511987654320" || !first.Accepted {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Buttons) != 1 || first.Buttons[0].Postback != "vz:m0" || first.Buttons[0].Label != "Quero" {
		t.Fatalf("buttons = %+v", first.Buttons)
	}
	if len(first.Reports) != 3 || first.Reports[2].Status != string(carriermessaging.StatusRead) || first.Reports[2].BackendStatus != http.StatusOK {
		t.Fatalf("reports = %+v", first.Reports)
	}
	if second.Accepted || second.ErrorCode == 0 || len(second.Reports) != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func TestInboundControl(t *testing.T) {
	h := newHarness(t)
	res := h.post(t, "/_fake/inbound", map[string]string{"phone": "5511987654320", "to": "29292", "channel": "sms", "text": "SAIR"})
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("inbound = %d %s", res.StatusCode, body)
	}
	calls := h.backend.callsTo("inbound")
	if len(calls) != 1 || calls[0].headers.Get("CF-Connecting-IP") != sourceIP {
		t.Fatalf("calls = %+v", calls)
	}
	in, err := cmcom.DecodeInbound(calls[0].body)
	if err != nil {
		t.Fatalf("inbound does not decode: %v\n%s", err, calls[0].body)
	}
	if in.Channel != carriermessaging.ChannelSMS || in.To != "29292" || in.Phone != "5511987654320" || in.Text != "SAIR" || in.ProviderMessageID == "" {
		t.Fatalf("inbound = %+v", in)
	}
	rcsReply := h.post(t, "/_fake/inbound", map[string]string{"phone": "5511987654320", "to": "agent_1", "channel": "RCS", "text": "Oi", "in_reply_to": reference(9)})
	if rcsReply.StatusCode != http.StatusOK {
		t.Fatalf("rcs inbound = %d", rcsReply.StatusCode)
	}
	in, err = cmcom.DecodeInbound(h.backend.callsTo("inbound")[1].body)
	if err != nil || in.Channel != carriermessaging.ChannelRCS || in.To != "agent_1" || in.InReplyTo != reference(9) {
		t.Fatalf("rcs inbound = %+v, %v", in, err)
	}
}

func TestInboundControlRefusals(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown channel": {"phone": "5511987654320", "to": "29292", "channel": "fax", "text": "x"},
		"bad phone":       {"phone": "abc", "to": "29292", "channel": "sms", "text": "x"},
		"no text":         {"phone": "5511987654320", "to": "29292", "channel": "sms", "text": " "},
		"no receiver":     {"phone": "5511987654320", "to": "", "channel": "sms", "text": "x"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if res := h.post(t, "/_fake/inbound", body); res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", res.StatusCode)
			}
			if n := len(h.backend.callsTo("inbound")); n != 0 {
				t.Fatalf("backend called %d times", n)
			}
		})
	}
}

func TestTapControl(t *testing.T) {
	h := newHarness(t)
	h.sendRCS(t, 1, "5511987654320")
	h.sendSMS(t, 2, "5511987654320")
	res := h.post(t, "/_fake/tap", map[string]string{"reference": reference(1), "postback": "vz:m0", "label": "Quero"})
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("tap = %d %s", res.StatusCode, body)
	}
	calls := h.backend.callsTo("events")
	if len(calls) != 1 || calls[0].headers.Get("X-Forwarded-For") != sourceIP {
		t.Fatalf("calls = %+v", calls)
	}
	click, ok, err := cmcom.DecodeEvent(calls[0].body)
	if err != nil || !ok {
		t.Fatalf("event does not decode: %v %v", ok, err)
	}
	if click.Reference != reference(1) || click.Postback != "vz:m0" || click.Label != "Quero" || click.To != "agent_1" || click.Phone != "5511987654320" || click.EventID == "" {
		t.Fatalf("click = %+v", click)
	}
	refusals := map[string]map[string]string{
		"unknown reference": {"reference": reference(7), "postback": "vz:m0"},
		"sms reference":     {"reference": reference(2), "postback": "vz:m0"},
		"no postback":       {"reference": reference(1), "postback": ""},
	}
	for name, body := range refusals {
		if res := h.post(t, "/_fake/tap", body); res.StatusCode < 400 {
			t.Fatalf("%s: status = %d", name, res.StatusCode)
		}
	}
	if n := len(h.backend.callsTo("events")); n != 1 {
		t.Fatalf("refused taps reached the backend: %d", n)
	}
}

func TestControlPage(t *testing.T) {
	h := newHarness(t)
	h.sendRCS(t, 1, "5511987654320")
	res, err := http.Get(h.server.URL + "/_fake")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	page := string(body)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	for _, want := range []string{reference(1), "5511987654320", "vz:m0", "Quero", "/_fake/inbound", "/_fake/tap"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page misses %q", want)
		}
	}
	for _, external := range []string{"<script src", "<link", "http://", "https://"} {
		if strings.Contains(page, external) {
			t.Fatalf("page must not load %q", external)
		}
	}
}

func TestControlRoutesRefuseOtherMethods(t *testing.T) {
	h := newHarness(t)
	res, err := http.Get(h.server.URL + "/_fake/inbound")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestOnboardingProgression(t *testing.T) {
	h := newHarness(t)
	s := rcs.Sender{
		BrandName: "Padaria Sol", Description: "Atualizações da Padaria Sol",
		LogoURL: "https://cdn.example.com/logo.png", BannerURL: "https://cdn.example.com/banner.png",
		Color: "#FF8800", UseCase: rcs.UseCaseTransactional, Sector: rcs.SectorRetail,
		ContactName: "Maria", ContactEmail: "maria@padariasol.com.br", ContactPhone: "+5511987654321",
		WebsiteURL: "https://padariasol.com.br", PrivacyURL: "https://padariasol.com.br/p", TermsURL: "https://padariasol.com.br/t",
		CNPJ: "12345678000195", BillingCategory: rcs.BillingNonConversational,
	}
	sub, err := h.client.SubmitSender(context.Background(), s)
	if err != nil {
		t.Fatalf("SubmitSender: %v", err)
	}
	s.ProviderRequestID, s.ProviderProfileID = sub.RequestID, sub.ProfileID
	for i, want := range []rcs.ProviderState{rcs.ProviderPending, rcs.ProviderAwaitingBrand, rcs.ProviderLaunched, rcs.ProviderLaunched} {
		got, err := h.client.SenderStatus(context.Background(), s)
		if err != nil || got.State != want {
			t.Fatalf("poll %d = %+v, %v; want %s", i, got, err, want)
		}
	}
}
