package carriermessaging

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	cm "vozko/domain/carriermessaging"
	"vozko/domain/webhook"
	cmuc "vozko/usecases/carriermessaging"
)

const testToken = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

type fakeProvider struct {
	allowed  netip.Prefix
	reports  []cm.StatusReport
	inbound  cm.Inbound
	click    cm.SuggestionClick
	clickOK  bool
	decodeEr error
}

func (f *fakeProvider) DecodeStatusReports(string, []byte) ([]cm.StatusReport, error) {
	return f.reports, f.decodeEr
}
func (f *fakeProvider) DecodeInbound([]byte) (cm.Inbound, error) { return f.inbound, f.decodeEr }
func (f *fakeProvider) DecodeEvent([]byte) (cm.SuggestionClick, bool, error) {
	return f.click, f.clickOK, f.decodeEr
}
func (f *fakeProvider) FromAllowedSource(ip netip.Addr) bool { return f.allowed.Contains(ip) }

type published struct {
	topic   string
	payload []byte
}

type fakePublisher struct {
	sent []published
	err  error
}

func (p *fakePublisher) Publish(topic string, payload []byte) error {
	if p.err != nil {
		return p.err
	}
	p.sent = append(p.sent, published{topic, payload})
	return nil
}

func serve(t *testing.T, h *WebhookHandler, path, ip string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc(WebhookPath, h.Handle).Methods(http.MethodPost)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
	req.Header.Set("CF-Connecting-IP", ip)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func newWebhookHandler(t *testing.T, p *fakeProvider, pub *fakePublisher) *WebhookHandler {
	t.Helper()
	h, err := NewWebhookHandler(testToken, map[string]ProviderWebhooks{"cmcom": p}, pub)
	if err != nil {
		t.Fatalf("NewWebhookHandler = %v", err)
	}
	return h
}

func TestWebhookQueuesEachStatusReport(t *testing.T) {
	p := &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21"), reports: []cm.StatusReport{{Reference: "a", Status: cm.StatusDelivered}, {Reference: "b", Status: cm.StatusRead}}}
	pub := &fakePublisher{}
	rec := serve(t, newWebhookHandler(t, p, pub), "/webhooks/carrier/cmcom/"+testToken+"/status", "31.169.56.10")
	if rec.Code != http.StatusOK || len(pub.sent) != 2 || pub.sent[0].topic != webhook.TopicCarrierStatus {
		t.Fatalf("code = %d, sent = %+v", rec.Code, pub.sent)
	}
	var report cm.StatusReport
	if err := cmuc.DecodeQueued(pub.sent[1].payload, &report); err != nil || report.Reference != "b" {
		t.Fatalf("queued = %+v, %v", report, err)
	}
}

func TestWebhookRoutesInboundAndEvents(t *testing.T) {
	p := &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21"), inbound: cm.Inbound{ProviderMessageID: "mo"}, click: cm.SuggestionClick{EventID: "ev"}, clickOK: true}
	pub := &fakePublisher{}
	h := newWebhookHandler(t, p, pub)
	serve(t, h, "/webhooks/carrier/cmcom/"+testToken+"/inbound", "31.169.56.10")
	serve(t, h, "/webhooks/carrier/cmcom/"+testToken+"/events", "31.169.56.10")
	if len(pub.sent) != 2 || pub.sent[0].topic != webhook.TopicCarrierInbound || pub.sent[1].topic != webhook.TopicCarrierEvent {
		t.Fatalf("sent = %+v", pub.sent)
	}
}

func TestWebhookIgnoresEventsItDoesNotUse(t *testing.T) {
	p := &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21")}
	pub := &fakePublisher{}
	rec := serve(t, newWebhookHandler(t, p, pub), "/webhooks/carrier/cmcom/"+testToken+"/events", "31.169.56.10")
	if rec.Code != http.StatusOK || len(pub.sent) != 0 {
		t.Fatalf("code = %d, sent = %d", rec.Code, len(pub.sent))
	}
}

func TestWebhookRefusesStrangers(t *testing.T) {
	p := &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21"), reports: []cm.StatusReport{{Reference: "a"}}}
	cases := []struct {
		name, path, ip string
		want           int
	}{
		{"wrong token", "/webhooks/carrier/cmcom/" + strings.Repeat("x", 43) + "/status", "31.169.56.10", http.StatusNotFound},
		{"outside the provider ranges", "/webhooks/carrier/cmcom/" + testToken + "/status", "8.8.8.8", http.StatusNotFound},
		{"unknown provider", "/webhooks/carrier/acme/" + testToken + "/status", "31.169.56.10", http.StatusNotFound},
		{"unknown kind", "/webhooks/carrier/cmcom/" + testToken + "/other", "31.169.56.10", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			rec := serve(t, newWebhookHandler(t, p, pub), tc.path, tc.ip)
			if rec.Code != tc.want || len(pub.sent) != 0 {
				t.Fatalf("code = %d, sent = %d", rec.Code, len(pub.sent))
			}
		})
	}
}

func TestWebhookRefusesUndecodableBodiesAndReportsQueueOutages(t *testing.T) {
	p := &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21"), decodeEr: errors.New("bad json")}
	rec := serve(t, newWebhookHandler(t, p, &fakePublisher{}), "/webhooks/carrier/cmcom/"+testToken+"/status", "31.169.56.10")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
	p = &fakeProvider{allowed: netip.MustParsePrefix("31.169.56.0/21"), reports: []cm.StatusReport{{Reference: "a"}}}
	rec = serve(t, newWebhookHandler(t, p, &fakePublisher{err: errors.New("queue down")}), "/webhooks/carrier/cmcom/"+testToken+"/status", "31.169.56.10")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, the provider must retry when nothing was queued", rec.Code)
	}
}

func TestWebhookTokenMustBeStrong(t *testing.T) {
	if _, err := NewWebhookHandler("short", map[string]ProviderWebhooks{"cmcom": &fakeProvider{}}, &fakePublisher{}); !errors.Is(err, ErrWebhookTokenWeak) {
		t.Fatalf("NewWebhookHandler = %v", err)
	}
}
