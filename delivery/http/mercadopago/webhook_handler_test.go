package mercadopagohttp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/webhook"
	"vozko/infra/mercadopago"
)

const secret = "8f4b2c1d9e3a5f7b0c2d4e6f8a1b3c5d"

type stubPublisher struct {
	topic   string
	payload []byte
	calls   int
	err     error
}

func (p *stubPublisher) Publish(topic string, payload []byte) error {
	p.calls++
	p.topic = topic
	p.payload = append([]byte(nil), payload...)
	return p.err
}

func signHeader(t *testing.T, dataID, requestID string, ts int64) string {
	t.Helper()
	tsStr := strconv.FormatInt(ts, 10)
	manifest := ""
	if dataID != "" {
		manifest += "id:" + dataID + ";"
	}
	if requestID != "" {
		manifest += "request-id:" + requestID + ";"
	}
	manifest += "ts:" + tsStr + ";"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(manifest))
	return fmt.Sprintf("ts=%s,v1=%s", tsStr, hex.EncodeToString(mac.Sum(nil)))
}

func notificationBody(dataID string) string {
	return fmt.Sprintf(`{"id":112233,"live_mode":true,"type":"payment","action":"payment.updated","data":{"id":%q}}`, dataID)
}

// signedRequest builds an authentic notification the way Mercado Pago would.
func signedRequest(t *testing.T, dataID string) *http.Request {
	t.Helper()
	ts := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id="+dataID+"&type=payment",
		strings.NewReader(notificationBody(dataID)))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, dataID, "req-abc", ts))
	return req
}

func TestHandleWebhook_AcceptsAndEnqueuesValidNotification(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, signedRequest(t, "1234567890"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected exactly one publish, got %d", pub.calls)
	}
	if pub.topic != webhook.TopicMercadoPagoPayment {
		t.Fatalf("topic: got %q", pub.topic)
	}

	var queued mercadopago.Notification
	if err := json.Unmarshal(pub.payload, &queued); err != nil {
		t.Fatalf("queued payload is not a notification: %v", err)
	}
	if queued.ResourceID() != "1234567890" || !queued.IsPayment() {
		t.Fatalf("queued notification: %+v", queued)
	}
	if queued.Action != "payment.updated" {
		t.Fatalf("action lost in normalization: %q", queued.Action)
	}
}

// TestHandleWebhook_NormalizesLegacyIPN: the legacy form carries the id only in the
// query string, which does not survive the queue, so it must be folded into the body.
func TestHandleWebhook_NormalizesLegacyIPN(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?topic=payment&id=555", strings.NewReader(""))
	req.Header.Set("x-request-id", "req-legacy")
	req.Header.Set("x-signature", signHeader(t, "555", "req-legacy", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var queued mercadopago.Notification
	if err := json.Unmarshal(pub.payload, &queued); err != nil {
		t.Fatalf("queued payload invalid: %v", err)
	}
	if queued.ResourceID() != "555" {
		t.Fatalf("query id not folded into the queued body: %+v", queued)
	}
	if !queued.IsPayment() {
		t.Fatalf("legacy topic not normalized: %q", queued.NormalizedType())
	}
}

func TestHandleWebhook_RejectsUnsigned(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=1", strings.NewReader(notificationBody("1")))
	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("an unsigned request must never be enqueued")
	}
}

func TestHandleWebhook_RejectsWrongSecret(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, "a-completely-different-secret")

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, signedRequest(t, "1234567890"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("must not enqueue on signature mismatch")
	}
}

// TestHandleWebhook_RejectsSwappedDataID is the forgery case that matters: sign for a
// payment you control, then point the request at someone else's.
func TestHandleWebhook_RejectsSwappedDataID(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=9999999999&type=payment",
		strings.NewReader(notificationBody("9999999999")))
	req.Header.Set("x-request-id", "req-abc")
	// Signature computed for a DIFFERENT payment id.
	req.Header.Set("x-signature", signHeader(t, "1111111111", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("must not enqueue a forged notification")
	}
}

// TestHandleWebhook_SignsOverQueryNotBody: the body id is attacker-controlled and
// unsigned, so a request whose signature covers only the query must still be judged on
// the query value.
func TestHandleWebhook_SignsOverQueryNotBody(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	// Authentic signature for query id 111; body claims 999.
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=111&type=payment",
		strings.NewReader(notificationBody("999")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "111", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	// The signature is valid for the query id, so the request is authentic and
	// accepted. The body id is what gets queued, which is safe because the resolver
	// fetches that payment from Mercado Pago and acts only on what the API returns.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebhook_FailsClosedWithoutSecret(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, "")

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, signedRequest(t, "1234567890"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unconfigured secret must reject everything, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("must not enqueue when unconfigured")
	}
}

func TestHandleWebhook_DoesNotLeakRejectionReason(t *testing.T) {
	h := NewWebhookHandler(&stubPublisher{}, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=1", strings.NewReader(notificationBody("1")))
	req.Header.Set("x-signature", "ts=1,v1=deadbeef")
	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	body := rec.Body.String()
	for _, leak := range []string{"SignatureMismatch", "MissingSecret", "MissingHash", "manifest"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks the rejection reason %q: %s", leak, body)
		}
	}
}

func TestHandleWebhook_AnswersDashboardProbe(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	req := httptest.NewRequest(http.MethodGet, "/webhooks/mercadopago", nil)
	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the GET probe to be answered 200, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("a probe must not enqueue anything")
	}
}

// TestHandleWebhook_AuthenticButUnusableIsAcknowledged: retrying a permanently broken
// notification would make Mercado Pago resend it for hours.
func TestHandleWebhook_AuthenticButUnusableIsAcknowledged(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	// Signed with no data.id and carrying a body with no id either.
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago", strings.NewReader(`{"type":"payment"}`))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 so Mercado Pago stops retrying, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("an unusable notification must not be enqueued")
	}
}

// TestHandleWebhook_QueueFailureIsRetryable: a 5xx makes Mercado Pago retry, which is
// exactly right when the queue is the broken part.
func TestHandleWebhook_QueueFailureIsRetryable(t *testing.T) {
	pub := &stubPublisher{err: errors.New("rabbit down")}
	h := NewWebhookHandler(pub, secret)

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, signedRequest(t, "1234567890"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 so the notification is retried, got %d", rec.Code)
	}
}

func TestHandleWebhook_ToleranceRejectsStaleWhenEnabled(t *testing.T) {
	pub := &stubPublisher{}
	now := time.Now()
	h := NewWebhookHandler(pub, secret,
		WithSignatureTolerance(5*time.Minute),
		WithClock(func() time.Time { return now }),
	)

	stale := now.Add(-time.Hour).UnixMilli()
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=1&type=payment", strings.NewReader(notificationBody("1")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "1", "req-abc", stale))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a stale signature, got %d", rec.Code)
	}
}

// TestHandleWebhook_ToleranceOffAcceptsOldRetry: the default must accept Mercado Pago's
// hours-later retries, which reuse the original signature.
func TestHandleWebhook_ToleranceOffAcceptsOldRetry(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	old := time.Now().Add(-6 * time.Hour).UnixMilli()
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=1&type=payment", strings.NewReader(notificationBody("1")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "1", "req-abc", old))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("an old authentic retry must be accepted by default, got %d", rec.Code)
	}
	if pub.calls != 1 {
		t.Fatalf("expected the retry to be enqueued, got %d publishes", pub.calls)
	}
}

func TestHandleWebhook_AcceptsIDQueryParamFallback(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	// Some configurations send "id" rather than "data.id"; the signature covers
	// whichever one arrives.
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?id=42&type=payment", strings.NewReader(notificationBody("42")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "42", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterPublicRoutes(t *testing.T) {
	pub := &stubPublisher{}
	r := mux.NewRouter()
	RegisterPublicRoutes(r, NewWebhookHandler(pub, secret))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, signedRequest(t, "1234567890"))
	if rec.Code != http.StatusOK {
		t.Fatalf("route not mounted, got %d", rec.Code)
	}

	// Methods other than GET/POST are not accepted.
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, "/webhooks/mercadopago", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE, got %d", rec2.Code)
	}
}

// TestRegisterPublicRoutes_NilHandlerMountsNothing: when Asaas is the active provider
// the route should not exist at all, rather than 401 on every call.
func TestRegisterPublicRoutes_NilHandlerMountsNothing(t *testing.T) {
	r := mux.NewRouter()
	RegisterPublicRoutes(r, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when the provider is not Mercado Pago, got %d", rec.Code)
	}
}

func TestHandleWebhook_OversizedBodyIsTruncatedNotHung(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	huge := strings.Repeat("a", (1<<20)+1024)
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=1&type=payment", strings.NewReader(huge))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "1", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	// The body is unparseable once truncated, but the request was authentic, so it is
	// acknowledged rather than retried forever.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("a truncated body must not be enqueued")
	}
}

// TestHandleWebhook_AcceptsBodySignedNotification is the production regression: Mercado
// Pago sent no data.id query parameter and signed over the body id instead. Rejecting
// it lost a paid invoice.
func TestHandleWebhook_AcceptsBodySignedNotification(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago",
		strings.NewReader(notificationBody("1234567890")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "1234567890", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected the notification to be enqueued, got %d publishes", pub.calls)
	}
}

// TestHandleWebhook_QueuesTheSignedIDNotTheBodyID closes the verify-one/act-on-another
// gap: when the signature covers one payment and the body names a different one, the
// consumer must be handed the id that was actually signed.
func TestHandleWebhook_QueuesTheSignedIDNotTheBodyID(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	// Authentic signature over query id 111; body claims 999.
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=111&type=payment",
		strings.NewReader(notificationBody("999")))
	req.Header.Set("x-request-id", "req-abc")
	req.Header.Set("x-signature", signHeader(t, "111", "req-abc", ts))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var queued mercadopago.Notification
	if err := json.Unmarshal(pub.payload, &queued); err != nil {
		t.Fatalf("queued payload invalid: %v", err)
	}
	if queued.ResourceID() != "111" {
		t.Fatalf("must queue the SIGNED id 111, got %q", queued.ResourceID())
	}
}

// TestHandleWebhook_ForgedSignatureStillRejectedWithBothSources: accepting more id
// sources must not let an unsigned notification through.
func TestHandleWebhook_ForgedSignatureStillRejectedWithBothSources(t *testing.T) {
	pub := &stubPublisher{}
	h := NewWebhookHandler(pub, secret)

	ts := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodPost,
		"/webhooks/mercadopago?data.id=555&type=payment",
		strings.NewReader(notificationBody("555")))
	req.Header.Set("x-request-id", "req-abc")
	// Signed with a secret the attacker chose.
	mac := hmac.New(sha256.New, []byte("attacker-secret"))
	mac.Write([]byte("id:555;request-id:req-abc;ts:" + strconv.FormatInt(ts, 10) + ";"))
	req.Header.Set("x-signature", fmt.Sprintf("ts=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil))))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if pub.calls != 0 {
		t.Fatal("a forged notification must never be enqueued")
	}
}
