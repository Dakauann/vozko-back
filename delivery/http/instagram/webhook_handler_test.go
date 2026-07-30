package instagram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vozko/domain/webhook"
)

const (
	testIGSecret = "instagram-app-secret"
	testWASecret = "whatsapp-app-secret"
	testVerify   = "verify-token"
)

// publishedMessage records one enqueued entry.
type publishedMessage struct {
	Topic   string
	Payload []byte
}

type fakePublisher struct {
	Published []publishedMessage
	Err       error
}

func (f *fakePublisher) Publish(topic string, payload []byte) error {
	if f.Err != nil {
		return f.Err
	}
	f.Published = append(f.Published, publishedMessage{Topic: topic, Payload: payload})
	return nil
}

var _ webhook.PublishWebhookUseCase = (*fakePublisher)(nil)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// fixture loads a payload captured verbatim from Meta's documentation.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "domain", "instagram", "testdata", "webhooks", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func newTestHandler(pub *fakePublisher) *WebhookHandler {
	// Both secrets are registered: the Instagram API setup has its own app secret,
	// and the docs do not state unambiguously which one signs Instagram webhooks.
	return NewWebhookHandler(pub, []string{testIGSecret, testWASecret}, testVerify)
}

func postWebhook(t *testing.T, h *WebhookHandler, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/instagram", strings.NewReader(string(body)))
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

// ---------------------------------------------------------------- handshake

// TestVerify_EchoesChallengeVerbatim: the challenge must come back byte-for-byte.
// Parsing it to an int and reprinting risks changing the bytes.
func TestVerify_EchoesChallengeVerbatim(t *testing.T) {
	h := newTestHandler(&fakePublisher{})

	// Note the DOTTED parameter names: hub.mode, not hub_mode.
	req := httptest.NewRequest(http.MethodGet,
		"/webhooks/instagram?hub.mode=subscribe&hub.challenge=1158201444&hub.verify_token="+testVerify, nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "1158201444" {
		t.Errorf("body = %q, want the challenge echoed verbatim", got)
	}
}

func TestVerify_RejectsWrongToken(t *testing.T) {
	h := newTestHandler(&fakePublisher{})

	req := httptest.NewRequest(http.MethodGet,
		"/webhooks/instagram?hub.mode=subscribe&hub.challenge=123&hub.verify_token=wrong", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestVerify_FailsClosedWithoutConfiguredToken: an unconfigured endpoint must not
// accept anyone's subscription.
func TestVerify_FailsClosedWithoutConfiguredToken(t *testing.T) {
	h := NewWebhookHandler(&fakePublisher{}, []string{testIGSecret}, "")

	req := httptest.NewRequest(http.MethodGet,
		"/webhooks/instagram?hub.mode=subscribe&hub.challenge=123&hub.verify_token=anything", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestVerify_RejectsWrongMode(t *testing.T) {
	h := newTestHandler(&fakePublisher{})

	req := httptest.NewRequest(http.MethodGet,
		"/webhooks/instagram?hub.mode=unsubscribe&hub.challenge=123&hub.verify_token="+testVerify, nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------- signature

func TestReceive_AcceptsInstagramAppSecret(t *testing.T) {
	pub := &fakePublisher{}
	body := fixture(t, "text_dm.json")

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(pub.Published) != 1 {
		t.Fatalf("published %d entries, want 1", len(pub.Published))
	}
}

// TestReceive_AcceptsEitherRegisteredSecret is what de-risks the documentation
// ambiguity: whichever secret Meta actually signs with, verification succeeds.
func TestReceive_AcceptsEitherRegisteredSecret(t *testing.T) {
	body := fixture(t, "text_dm.json")

	for name, secret := range map[string]string{
		"instagram app secret": testIGSecret,
		"whatsapp app secret":  testWASecret,
	} {
		pub := &fakePublisher{}
		rec := postWebhook(t, newTestHandler(pub), body, sign(secret, body))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", name, rec.Code)
		}
	}
}

func TestReceive_RejectsBadSignature(t *testing.T) {
	pub := &fakePublisher{}
	body := fixture(t, "text_dm.json")

	rec := postWebhook(t, newTestHandler(pub), body, sign("some-other-secret", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(pub.Published) != 0 {
		t.Error("an unverified payload was enqueued")
	}
}

func TestReceive_RejectsMissingSignature(t *testing.T) {
	pub := &fakePublisher{}
	rec := postWebhook(t, newTestHandler(pub), fixture(t, "text_dm.json"), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(pub.Published) != 0 {
		t.Error("an unsigned payload was enqueued")
	}
}

// TestReceive_RefusesWhenNoSecretConfigured: the WhatsApp handler logs a warning and
// processes anyway; that default is deliberately not copied here.
func TestReceive_RefusesWhenNoSecretConfigured(t *testing.T) {
	pub := &fakePublisher{}
	h := NewWebhookHandler(pub, nil, testVerify)
	body := fixture(t, "text_dm.json")

	rec := postWebhook(t, h, body, sign(testIGSecret, body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(pub.Published) != 0 {
		t.Error("payload processed with no configured secret")
	}
}

// TestReceive_SignatureIsOverExactBytes: re-serialising the body before hashing
// produces a different signature, so a payload whose bytes differ from what was
// signed must be rejected even if the JSON is equivalent.
func TestReceive_SignatureIsOverExactBytes(t *testing.T) {
	pub := &fakePublisher{}
	body := fixture(t, "text_dm.json")

	// Re-marshal to equivalent-but-different bytes, keeping the original signature.
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	remarshalled, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := postWebhook(t, newTestHandler(pub), remarshalled, sign(testIGSecret, body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (signature must cover the exact bytes)", rec.Code)
	}
}

// ---------------------------------------------------------------- routing

// TestReceive_RoutesByEventFamily keeps a burst of comment moderation from delaying
// DM delivery, since each family has its own queue and prefetch.
func TestReceive_RoutesByEventFamily(t *testing.T) {
	cases := []struct {
		fixture string
		topic   string
	}{
		{"text_dm.json", webhook.TopicInstagramMessage},
		{"reaction_react.json", webhook.TopicInstagramMessage},
		{"standby.json", webhook.TopicInstagramMessage},
		{"comment_ig_login.json", webhook.TopicInstagramComment},
		{"comment_fb_login.json", webhook.TopicInstagramComment},
		// An undocumented field must still be enqueued so it can be logged, not
		// silently dropped.
		{"unknown_field.json", webhook.TopicInstagramAccount},
	}

	for _, c := range cases {
		pub := &fakePublisher{}
		body := fixture(t, c.fixture)

		rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", c.fixture, rec.Code)
			continue
		}
		if len(pub.Published) != 1 {
			t.Errorf("%s: published %d, want 1", c.fixture, len(pub.Published))
			continue
		}
		if pub.Published[0].Topic != c.topic {
			t.Errorf("%s: topic = %q, want %q", c.fixture, pub.Published[0].Topic, c.topic)
		}
	}
}

// TestReceive_SplitsMultiAccountBatchPerEntry is what gives per-tenant failure
// isolation: one POST can span several accounts, so each entry becomes its own
// independently retryable queue message.
func TestReceive_SplitsMultiAccountBatchPerEntry(t *testing.T) {
	pub := &fakePublisher{}
	body := fixture(t, "multi_account_batch.json")

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(pub.Published) != 2 {
		t.Fatalf("published %d messages, want 2 (one per entry)", len(pub.Published))
	}

	seen := map[string]bool{}
	for _, msg := range pub.Published {
		var env struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(msg.Payload, &env); err != nil {
			t.Fatalf("published payload is not a single entry envelope: %v", err)
		}
		seen[env.Entry.ID] = true
	}
	for _, want := range []string{"IGID_A", "IGID_B"} {
		if !seen[want] {
			t.Errorf("entry for %s was not published separately", want)
		}
	}
}

func TestReceive_AcceptsArrayWrappedPayload(t *testing.T) {
	pub := &fakePublisher{}
	body := fixture(t, "top_level_array.json")

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(pub.Published) != 1 {
		t.Fatalf("published %d, want 1", len(pub.Published))
	}
}

// TestReceive_PublishFailureReturns500: acknowledging an event we failed to persist
// would lose it. A 500 makes Meta redeliver, which is safe because the pipeline is
// idempotent.
func TestReceive_PublishFailureReturns500(t *testing.T) {
	pub := &fakePublisher{Err: errAlwaysFails}
	body := fixture(t, "text_dm.json")

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so Meta retries", rec.Code)
	}
}

// TestReceive_UndecodablePayloadIsAcked: a body that will never parse must not be
// retried for 36 hours; it is acked and logged instead.
func TestReceive_UndecodablePayloadIsAcked(t *testing.T) {
	pub := &fakePublisher{}
	body := []byte("this is not json")

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (acked, not retried forever)", rec.Code)
	}
	if len(pub.Published) != 0 {
		t.Error("an undecodable payload was enqueued")
	}
}

func TestHandle_RejectsOtherMethods(t *testing.T) {
	h := newTestHandler(&fakePublisher{})
	req := httptest.NewRequest(http.MethodPut, "/webhooks/instagram", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

type constError string

func (e constError) Error() string { return string(e) }

const errAlwaysFails = constError("broker unavailable")
