package unofficial_whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	uw "vozko/domain/unofficial_whatsapp"
)

// This provider authenticates NOTHING: it does not sign the body, and its
// webhook config accepts only a URL, so we cannot ask it to echo a header. Every
// test here pins one of the layers standing in for that missing signature.

type stubInstances struct {
	instance *uw.Instance
	// lookups records the digests the handler resolved by, so a test can prove
	// the raw token never reaches the database.
	lookups []string
}

func (s *stubInstances) FindByDeliveryTokenHash(_ context.Context, hash string) (*uw.Instance, error) {
	s.lookups = append(s.lookups, hash)
	if s.instance != nil && s.instance.DeliveryTokenHash == hash {
		return s.instance, nil
	}
	return nil, uw.ErrInstanceNotFound
}

type stubPublisher struct {
	topics  []string
	fail    bool
	payload []byte
}

func (s *stubPublisher) Publish(topic string, payload []byte) error {
	if s.fail {
		return context.DeadlineExceeded
	}
	s.topics = append(s.topics, topic)
	s.payload = payload
	return nil
}

func webhookFixture(t *testing.T) (*mux.Router, *stubInstances, *stubPublisher, string) {
	t.Helper()

	token, err := uw.GenerateDeliveryToken()
	if err != nil {
		t.Fatalf("GenerateDeliveryToken: %v", err)
	}
	instances := &stubInstances{instance: &uw.Instance{
		ID: "inst-1", ProviderInstanceID: "r18",
		Status: uw.StatusConnected, DeliveryTokenHash: uw.HashDeliveryToken(token),
	}}
	publisher := &stubPublisher{}

	router := mux.NewRouter()
	RegisterPublicRoutes(router, NewWebhookHandler(instances, publisher))
	return router, instances, publisher, token
}

func post(router *mux.Router, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		uw.WebhookPathPrefix+"/"+token, strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestWebhookAcceptsAValidDelivery(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	rec := post(router, token, `{"event":"messages","instance":"r18","data":{"messageid":"m1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(publisher.topics) != 1 {
		t.Fatalf("published %d times, want once", len(publisher.topics))
	}
	if !strings.Contains(publisher.topics[0], "message") {
		t.Errorf("topic = %q, want the message lane", publisher.topics[0])
	}
}

// An unknown token answers 401, never 404: distinguishing "malformed" from
// "unknown" tells a scanner which tokens exist.
func TestWebhookRejectsAnUnknownToken(t *testing.T) {
	router, _, publisher, _ := webhookFixture(t)

	rec := post(router, "not-a-real-token", `{"event":"messages","data":{}}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (a 404 would enumerate valid tokens)", rec.Code)
	}
	if len(publisher.topics) != 0 {
		t.Error("nothing may be published for an unauthenticated delivery")
	}
}

// The token is resolved through its DIGEST, so a dumped instances table yields
// no working URLs.
func TestWebhookResolvesByDigestNotByToken(t *testing.T) {
	router, instances, _, token := webhookFixture(t)

	post(router, token, `{"event":"messages","instance":"r18","data":{}}`)

	if len(instances.lookups) != 1 {
		t.Fatalf("lookups = %d, want 1", len(instances.lookups))
	}
	if instances.lookups[0] == token {
		t.Fatal("the raw token was used as the lookup key; a dumped row would yield a working URL")
	}
	if instances.lookups[0] != uw.HashDeliveryToken(token) {
		t.Error("lookup did not use the token digest")
	}
}

// The second factor. Without it the URL alone would be enough to inject events
// into any tenant's inbox.
func TestWebhookRejectsAMismatchedInstanceID(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	rec := post(router, token, `{"event":"messages","instance":"someone-elses","data":{}}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401: the body names another instance", rec.Code)
	}
	if len(publisher.topics) != 0 {
		t.Error("a forged instance id must not reach the queue")
	}
}

// A body with no instance id still processes: the field is a cross-check, not a
// requirement, and rejecting deliveries that omit it would break the channel if
// the vendor changed its envelope.
func TestWebhookToleratesAMissingInstanceID(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	rec := post(router, token, `{"event":"messages","data":{"messageid":"m1"}}`)
	if rec.Code != http.StatusOK || len(publisher.topics) != 1 {
		t.Errorf("status = %d, published = %d", rec.Code, len(publisher.topics))
	}
}

// A malformed body is ACKed so the provider stops retrying, and logged so the
// shape can be investigated — this vendor's payloads are undocumented, so an
// unexpected shape is information rather than an attack.
func TestWebhookAcksAnUndecodableBody(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	rec := post(router, token, `not json at all`)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: retrying an unparseable body forever helps nobody", rec.Code)
	}
	if len(publisher.topics) != 0 {
		t.Error("an undecodable body must not be queued")
	}
}

// A publish failure must answer non-2xx so the provider redelivers. This
// provider has NO replay endpoint, so acknowledging an event we failed to
// enqueue loses it permanently.
func TestWebhookAsksForRedeliveryWhenPublishFails(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)
	publisher.fail = true

	rec := post(router, token, `{"event":"messages","instance":"r18","data":{}}`)
	if rec.Code < 500 {
		t.Errorf("status = %d, want 5xx so the provider retries", rec.Code)
	}
}

// A history replay must not share the lane a live customer's message uses: a
// seven-day backfill would queue in front of someone waiting for an answer.
func TestWebhookRoutesHistoryToItsOwnLane(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	post(router, token, `{"event":"history","instance":"r18","data":[]}`)
	if len(publisher.topics) != 1 || !strings.Contains(publisher.topics[0], "history") {
		t.Errorf("topics = %v, want the history lane", publisher.topics)
	}
}

// An event kind the vendor adds lands on the catch-all rather than being
// refused, so it is logged instead of silently lost.
func TestWebhookRoutesUnknownEventsToTheCatchAll(t *testing.T) {
	router, _, publisher, token := webhookFixture(t)

	post(router, token, `{"event":"quantum_flux","instance":"r18","data":{}}`)
	if len(publisher.topics) != 1 || !strings.Contains(publisher.topics[0], "instance") {
		t.Errorf("topics = %v, want the catch-all lane", publisher.topics)
	}
}

func TestWebhookRejectsNonPost(t *testing.T) {
	router, _, _, token := webhookFixture(t)

	req := httptest.NewRequest(http.MethodGet, uw.WebhookPathPrefix+"/"+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The router itself refuses the method; either answer is correct, as long as
	// it is not a success.
	if rec.Code == http.StatusOK {
		t.Error("a GET must not be treated as a delivery")
	}
}

// A nil handler means the channel is disabled: registering a route whose method
// would nil-panic on the first request is worse than having no route.
func TestRegisterPublicRoutesNilHandler(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, nil)

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodPost, uw.WebhookPathPrefix+"/tok", nil)
	if router.Match(req, &match) {
		t.Error("no webhook route may exist when the channel is disabled")
	}
}
