package tiktok

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ttdomain "vozko/domain/tiktok"
	"vozko/domain/webhook"
)

type fakePublisher struct {
	mu       sync.Mutex
	messages map[string][][]byte
	err      error
}

func (f *fakePublisher) Publish(topic string, message []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.messages == nil {
		f.messages = map[string][][]byte{}
	}
	f.messages[topic] = append(f.messages[topic], message)
	return nil
}

type countingMetrics struct {
	mu     sync.Mutex
	events map[string]int
}

func (m *countingMetrics) AddTikTokEvent(stage, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = map[string]int{}
	}
	m.events[stage+":"+outcome]++
}

const webhookSecret = "app-secret"

var webhookNow = time.Unix(1701090900, 0)

func sign(body string, at time.Time) string {
	compact := strings.Join(strings.Fields(body), "")
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(ts + "." + compact))
	return "t=" + ts + ",s=" + hex.EncodeToString(mac.Sum(nil))
}

func newWebhookForTest(pub *fakePublisher) (*WebhookHandler, *countingMetrics) {
	metrics := &countingMetrics{}
	h := NewWebhookHandler(webhookSecret, pub, metrics)
	h.now = func() time.Time { return webhookNow }
	return h, metrics
}

const textEvent = `{"client_key":"7043848340043202562","event":"im_receive_msg","create_time":1701090882,"user_openid":"_000abc","content":"{\"conversation_id\":\"c1\",\"message_id\":\"m1\",\"unique_identifier\":\"p1\",\"timestamp\":1701090879815,\"type\":\"text\",\"text\":{\"body\":\"oi\"}}"}`

const commentEvent = `{"client_key":"k","event":"comment.update","create_time":1701090882,"user_openid":"_000abc","content":"{\"comment_id\":7247303576418566913,\"video_id\":7203946942097902849,\"comment_type\":\"comment\",\"comment_action\":\"insert\",\"unique_identifier\":\"p\",\"timestamp\":1}"}`

func post(h *WebhookHandler, body, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok", strings.NewReader(body))
	req.Header.Set(signatureHeader, signature)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

func TestASignedMessageEventIsQueuedOnTheMessageTopic(t *testing.T) {
	pub := &fakePublisher{}
	h, metrics := newWebhookForTest(pub)
	rec := post(h, textEvent, sign(textEvent, webhookNow))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	queued := pub.messages[webhook.TopicTikTokMessage]
	if len(queued) != 1 {
		t.Fatalf("queued = %v", pub.messages)
	}
	var env ttdomain.Envelope
	if err := json.Unmarshal(queued[0], &env); err != nil || env.Event != "im_receive_msg" || env.UserOpenID != "_000abc" || !strings.Contains(env.Content, "m1") {
		t.Fatalf("envelope %+v err %v", env, err)
	}
	if metrics.events["webhook:accepted"] != 1 {
		t.Errorf("metrics = %v", metrics.events)
	}
}

func TestCommentEventsGoToTheCommentTopic(t *testing.T) {
	pub := &fakePublisher{}
	h, _ := newWebhookForTest(pub)
	if rec := post(h, commentEvent, sign(commentEvent, webhookNow)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(pub.messages[webhook.TopicTikTokComment]) != 1 {
		t.Fatalf("queued = %v", pub.messages)
	}
}

func TestAPrettyPrintedBodyVerifiesAgainstItsCompactForm(t *testing.T) {
	pub := &fakePublisher{}
	h, _ := newWebhookForTest(pub)
	pretty := "{\n  \"client_key\": \"k\",\n  \"event\": \"im_receive_msg\",\n  \"create_time\": 1701090882,\n  \"user_openid\": \"_000abc\",\n  \"content\": \"{}\"\n}"
	if rec := post(h, pretty, sign(pretty, webhookNow)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDeliveriesThatFailVerificationAreRefusedAndNeverQueued(t *testing.T) {
	pub := &fakePublisher{}
	h, metrics := newWebhookForTest(pub)
	cases := map[string]string{
		"wrong signature": "t=" + strconv.FormatInt(webhookNow.Unix(), 10) + ",s=00",
		"missing header":  "",
		"stale":           sign(textEvent, webhookNow.Add(-10*time.Minute)),
		"tampered":        sign(strings.Replace(textEvent, "oi", "tchau", 1), webhookNow),
	}
	for name, signature := range cases {
		if rec := post(h, textEvent, signature); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d", name, rec.Code)
		}
	}
	if len(pub.messages) != 0 {
		t.Fatalf("refused deliveries were queued: %v", pub.messages)
	}
	if metrics.events["webhook:signature_stale"] != 1 || metrics.events["webhook:signature_malformed"] != 1 {
		t.Errorf("metrics = %v", metrics.events)
	}
}

func TestUnhandledEventsAreAcknowledgedWithoutQueueing(t *testing.T) {
	pub := &fakePublisher{}
	h, _ := newWebhookForTest(pub)
	body := `{"client_key":"k","event":"share.video.publish_complete","create_time":1,"user_openid":"_000abc","content":"{}"}`
	if rec := post(h, body, sign(body, webhookNow)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(pub.messages) != 0 {
		t.Fatal("an unhandled event was queued")
	}
}

func TestAQueueFailureAsksTikTokToRetry(t *testing.T) {
	pub := &fakePublisher{err: errors.New("rabbit down")}
	h, _ := newWebhookForTest(pub)
	if rec := post(h, textEvent, sign(textEvent, webhookNow)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMalformedAndOversizedBodiesAreRefused(t *testing.T) {
	pub := &fakePublisher{}
	h, _ := newWebhookForTest(pub)
	if rec := post(h, "{not json", "t=1,s=00"); rec.Code != http.StatusBadRequest {
		t.Errorf("not json: %d", rec.Code)
	}
	huge := `{"x":"` + strings.Repeat("a", maxWebhookBody) + `"}`
	if rec := post(h, huge, "t=1,s=00"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge: %d", rec.Code)
	}
	noOpenID := `{"event":"im_receive_msg","create_time":1,"content":"{}"}`
	if rec := post(h, noOpenID, sign(noOpenID, webhookNow)); rec.Code != http.StatusBadRequest {
		t.Errorf("no open id: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/webhooks/tiktok", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("get: %d", rec.Code)
	}
}

func TestTheWebhookHandlerRequiresASecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an empty secret must stop the boot")
		}
	}()
	NewWebhookHandler(" ", &fakePublisher{}, &countingMetrics{})
}
