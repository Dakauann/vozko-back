package metawebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	mm "vozko/domain/metamessaging"
)

type published struct {
	topic   string
	payload []byte
}

type fakePublisher struct {
	items []published
	err   error
}

func (f *fakePublisher) Publish(topic string, payload []byte) error {
	if f.err != nil {
		return f.err
	}
	f.items = append(f.items, published{topic, payload})
	return nil
}

const secret = "meta-secret"

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func route(env *mm.EntryEnvelope) []string {
	var topics []string
	if len(env.Entry.Messaging) > 0 || len(env.Entry.Standby) > 0 {
		topics = append(topics, "msg")
	}
	if len(env.Entry.Changes) > 0 {
		topics = append(topics, "feed")
	}
	return topics
}

func newHandler(pub *fakePublisher, objects ...string) *Handler {
	return New(Config{
		Name:        "test-webhook",
		Publisher:   pub,
		Secrets:     []string{secret},
		VerifyToken: "verify",
		Objects:     objects,
		Route:       route,
	})
}

func post(h *Handler, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/x", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(body))
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

func TestMixedEntryIsPublishedToEveryRoutedTopic(t *testing.T) {
	pub := &fakePublisher{}
	body := []byte(`{"object":"page","entry":[{"id":"P","time":1,
		"messaging":[{"sender":{"id":"U"},"recipient":{"id":"P"},"timestamp":1,"message":{"mid":"m","text":"x"}}],
		"changes":[{"field":"feed","value":{"item":"comment","verb":"add"}}]}]}`)
	rec := post(newHandler(pub, "page"), body)
	if rec.Code != http.StatusOK || len(pub.items) != 2 {
		t.Fatalf("code=%d published=%d", rec.Code, len(pub.items))
	}
	if pub.items[0].topic != "msg" || pub.items[1].topic != "feed" {
		t.Fatalf("topics = %s, %s", pub.items[0].topic, pub.items[1].topic)
	}
	var env mm.EntryEnvelope
	if err := json.Unmarshal(pub.items[0].payload, &env); err != nil || env.Entry.ID != "P" || env.Object != "page" {
		t.Fatalf("payload = %s (%v)", pub.items[0].payload, err)
	}
}

func TestForeignObjectIsAckedWithoutPublishing(t *testing.T) {
	pub := &fakePublisher{}
	rec := post(newHandler(pub, "page"), []byte(`{"object":"instagram","entry":[{"id":"I","messaging":[{"sender":{"id":"U"},"recipient":{"id":"I"},"timestamp":1,"message":{"mid":"m"}}]}]}`))
	if rec.Code != http.StatusOK || len(pub.items) != 0 {
		t.Fatalf("code=%d published=%d", rec.Code, len(pub.items))
	}
}

func TestEntryWithoutRouteIsSkipped(t *testing.T) {
	pub := &fakePublisher{}
	rec := post(newHandler(pub), []byte(`{"object":"page","entry":[{"id":"P","time":1}]}`))
	if rec.Code != http.StatusOK || len(pub.items) != 0 {
		t.Fatalf("code=%d published=%d", rec.Code, len(pub.items))
	}
}

func TestPublishFailureAsksMetaToRetry(t *testing.T) {
	pub := &fakePublisher{err: errors.New("broker down")}
	rec := post(newHandler(pub), []byte(`{"object":"page","entry":[{"id":"P","messaging":[{"sender":{"id":"U"},"recipient":{"id":"P"},"timestamp":1,"message":{"mid":"m"}}]}]}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestBadSignatureIsRejected(t *testing.T) {
	pub := &fakePublisher{}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/x", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()
	newHandler(pub).Handle(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestHandshakeEchoesChallenge(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/webhooks/x?hub.mode=subscribe&hub.verify_token=verify&hub.challenge=42", nil)
	rec := httptest.NewRecorder()
	newHandler(&fakePublisher{}).Handle(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "42" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}
