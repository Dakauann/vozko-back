package mercadopago

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"testing"
	"time"

	"vozko/domain/payment"
)

const testSecret = "8f4b2c1d9e3a5f7b0c2d4e6f8a1b3c5d"

// signManifest reproduces what Mercado Pago does, so the tests assert against an
// independently built signature rather than against the implementation itself.
func signManifest(secret, manifest string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(manifest))
	return hex.EncodeToString(mac.Sum(nil))
}

func signatureHeader(secret, dataID, requestID string, ts int64) string {
	tsStr := strconv.FormatInt(ts, 10)
	parts := ""
	if dataID != "" {
		parts += "id:" + dataID + ";"
	}
	if requestID != "" {
		parts += "request-id:" + requestID + ";"
	}
	parts += "ts:" + tsStr + ";"
	return fmt.Sprintf("ts=%s,v1=%s", tsStr, signManifest(secret, parts))
}

func TestVerifySignature_ValidFullManifest(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req-abc", ts)

	if err := VerifySignature(header, "req-abc", "1234567890", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("expected a valid signature to pass, got %v", err)
	}
}

func TestVerifySignature_ValidWithoutRequestID(t *testing.T) {
	// Mercado Pago omits the request-id component entirely when the header is absent,
	// rather than signing an empty value.
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "", ts)

	if err := VerifySignature(header, "", "1234567890", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("expected pass without request-id, got %v", err)
	}
}

func TestVerifySignature_ValidWithoutDataID(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "", "req-abc", ts)

	if err := VerifySignature(header, "req-abc", "", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("expected pass without data.id, got %v", err)
	}
}

func TestVerifySignature_TimestampOnlyManifest(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "", "", ts)

	if err := VerifySignature(header, "", "", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("expected pass with a ts-only manifest, got %v", err)
	}
}

func TestVerifySignature_AlphanumericIDAcceptedEitherCase(t *testing.T) {
	// The docs say to lowercase an alphanumeric id; the official SDK hashes it
	// verbatim. Both must verify, or a future non-numeric id would silently reject
	// every webhook.
	ts := time.Now().UnixMilli()

	lowered := signatureHeader(testSecret, "abc-def", "req", ts)
	if err := VerifySignature(lowered, "req", "ABC-DEF", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("lowercased manifest should verify, got %v", err)
	}

	verbatim := signatureHeader(testSecret, "ABC-DEF", "req", ts)
	if err := VerifySignature(verbatim, "req", "ABC-DEF", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("verbatim manifest should verify, got %v", err)
	}
}

func TestVerifySignature_RejectsWrongSecret(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader("some-other-secret", "1234567890", "req-abc", ts)

	err := VerifySignature(header, "req-abc", "1234567890", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonSignatureMismatch)
}

func TestVerifySignature_RejectsTamperedDataID(t *testing.T) {
	// The core forgery attempt: sign for a payment you own, then point the request at
	// someone else's.
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1111111111", "req-abc", ts)

	err := VerifySignature(header, "req-abc", "9999999999", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonSignatureMismatch)
}

func TestVerifySignature_RejectsTamperedRequestID(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req-abc", ts)

	err := VerifySignature(header, "req-other", "1234567890", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonSignatureMismatch)
}

func TestVerifySignature_RejectsTamperedTimestamp(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req-abc", ts)
	// Replace only the advertised ts, leaving the hash intact.
	tampered := "ts=" + strconv.FormatInt(ts+1, 10) + header[len("ts="+strconv.FormatInt(ts, 10)):]

	err := VerifySignature(tampered, "req-abc", "1234567890", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonSignatureMismatch)
}

func TestVerifySignature_FailsClosedWithoutSecret(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader("", "1234567890", "req", ts)

	// Even a signature that is internally consistent with an empty secret must be
	// rejected: an unconfigured endpoint must never be an open one.
	err := VerifySignature(header, "req", "1234567890", "", 0, time.Now())
	assertSignatureReason(t, err, ReasonMissingSecret)
}

func TestVerifySignature_MissingHeader(t *testing.T) {
	err := VerifySignature("", "req", "1", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonMissingSignatureHeader)

	err = VerifySignature("   ", "req", "1", testSecret, 0, time.Now())
	assertSignatureReason(t, err, ReasonMissingSignatureHeader)
}

func TestVerifySignature_MalformedHeaders(t *testing.T) {
	cases := []struct {
		name   string
		header string
		reason SignatureReason
	}{
		{"no separators", "garbage", ReasonMalformedSignature},
		{"empty components", "ts=,v1=", ReasonMalformedSignature},
		{"unknown keys only", "foo=bar,baz=qux", ReasonMalformedSignature},
		{"hash without ts", "v1=" + signManifest(testSecret, "ts:1;"), ReasonMissingTimestamp},
		{"non-numeric ts", "ts=notanumber,v1=abcdef", ReasonMalformedSignature},
		{"ts without hash", "ts=1704908010000", ReasonMissingHash},
		{"unsupported version only", "ts=1704908010000,v2=abcdef", ReasonMissingHash},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := VerifySignature(c.header, "req", "1", testSecret, 0, time.Now())
			assertSignatureReason(t, err, c.reason)
		})
	}
}

func TestVerifySignature_IgnoresUnknownComponentsAndWhitespace(t *testing.T) {
	// A future added component must not break verification of the v1 hash.
	ts := time.Now().UnixMilli()
	base := signatureHeader(testSecret, "1234567890", "req-abc", ts)
	padded := " " + base + " , foo=bar "

	if err := VerifySignature(padded, " req-abc ", " 1234567890 ", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("expected tolerance of padding and extra components, got %v", err)
	}
}

func TestVerifySignature_ToleranceDisabledByDefaultAcceptsOldRetries(t *testing.T) {
	// Mercado Pago retries for hours without re-signing. With tolerance off, a
	// six-hour-old but authentic signature must still be accepted.
	old := time.Now().Add(-6 * time.Hour).UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req", old)

	if err := VerifySignature(header, "req", "1234567890", testSecret, 0, time.Now()); err != nil {
		t.Fatalf("an old but authentic retry must be accepted when tolerance is off, got %v", err)
	}
}

func TestVerifySignature_ToleranceRejectsOldWhenEnabled(t *testing.T) {
	now := time.Now()
	old := now.Add(-30 * time.Minute).UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req", old)

	err := VerifySignature(header, "req", "1234567890", testSecret, 5*time.Minute, now)
	assertSignatureReason(t, err, ReasonTimestampOutOfTolerance)

	// Inside the window it passes.
	fresh := now.Add(-time.Minute).UnixMilli()
	freshHeader := signatureHeader(testSecret, "1234567890", "req", fresh)
	if err := VerifySignature(freshHeader, "req", "1234567890", testSecret, 5*time.Minute, now); err != nil {
		t.Fatalf("a fresh signature must pass the tolerance check, got %v", err)
	}
}

func TestVerifySignature_ToleranceRejectsFarFutureTimestamp(t *testing.T) {
	now := time.Now()
	future := now.Add(30 * time.Minute).UnixMilli()
	header := signatureHeader(testSecret, "1", "req", future)

	err := VerifySignature(header, "req", "1", testSecret, 5*time.Minute, now)
	assertSignatureReason(t, err, ReasonTimestampOutOfTolerance)
}

func TestSignatureError_MatchesSentinel(t *testing.T) {
	err := VerifySignature("", "", "", testSecret, 0, time.Now())
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("SignatureError must satisfy errors.Is(ErrInvalidSignature), got %v", err)
	}
}

func assertSignatureReason(t *testing.T, err error, want SignatureReason) {
	t.Helper()
	var sigErr *SignatureError
	if !errors.As(err, &sigErr) {
		t.Fatalf("expected *SignatureError, got %T: %v", err, err)
	}
	if sigErr.Reason != want {
		t.Fatalf("expected reason %q, got %q", want, sigErr.Reason)
	}
}

func TestParseNotification_ModernEnvelope(t *testing.T) {
	body := []byte(`{
	  "id": 112233,
	  "live_mode": true,
	  "type": "payment",
	  "date_created": "2026-08-31T10:04:58.396-04:00",
	  "user_id": 44444,
	  "api_version": "v1",
	  "action": "payment.updated",
	  "data": {"id": "1234567890"}
	}`)

	n, err := ParseNotification(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ResourceID() != "1234567890" {
		t.Fatalf("resource id: got %q", n.ResourceID())
	}
	if !n.IsPayment() || n.NormalizedType() != "payment" {
		t.Fatalf("expected a payment notification, got %q", n.NormalizedType())
	}
	if n.Action != "payment.updated" {
		t.Fatalf("action: got %q", n.Action)
	}
	if notificationEventID(n) != "112233" {
		t.Fatalf("event id: got %q", notificationEventID(n))
	}
}

func TestParseNotification_LegacyIPNQueryOnly(t *testing.T) {
	// The legacy IPN sends no body at all: "?topic=payment&id=123".
	q := url.Values{"topic": {"payment"}, "id": {"1234567890"}}

	n, err := ParseNotification(nil, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ResourceID() != "1234567890" {
		t.Fatalf("resource id: got %q", n.ResourceID())
	}
	if !n.IsPayment() {
		t.Fatalf("expected a payment notification, got %q", n.NormalizedType())
	}
	// The event id falls back to the resource id when the envelope carries none.
	if notificationEventID(n) != "1234567890" {
		t.Fatalf("event id fallback: got %q", notificationEventID(n))
	}
}

func TestParseNotification_QueryFillsMissingBodyFields(t *testing.T) {
	q := url.Values{"data.id": {"555"}, "type": {"payment"}}
	n, err := ParseNotification([]byte(`{"action":"payment.created"}`), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ResourceID() != "555" || !n.IsPayment() {
		t.Fatalf("query fallback failed: %+v", n)
	}
}

func TestParseNotification_BodyWinsOverQuery(t *testing.T) {
	q := url.Values{"data.id": {"999"}}
	n, err := ParseNotification([]byte(`{"type":"payment","data":{"id":"111"}}`), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ResourceID() != "111" {
		t.Fatalf("body id should win, got %q", n.ResourceID())
	}
}

func TestParseNotification_ResourceURLForm(t *testing.T) {
	body := []byte(`{"topic":"payment","resource":"https://api.mercadolibre.com/collections/notifications/1234567890"}`)
	n, err := ParseNotification(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ResourceID() != "1234567890" {
		t.Fatalf("resource URL not reduced to an id: %q", n.ResourceID())
	}
}

func TestParseNotification_Errors(t *testing.T) {
	if _, err := ParseNotification([]byte("{not json"), nil); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if _, err := ParseNotification([]byte(`{"type":"payment"}`), nil); !errors.Is(err, ErrNotificationMissingResourceID) {
		t.Fatalf("expected ErrNotificationMissingResourceID, got %v", err)
	}
	if _, err := ParseNotification(nil, url.Values{}); !errors.Is(err, ErrNotificationMissingResourceID) {
		t.Fatalf("expected ErrNotificationMissingResourceID for an empty request, got %v", err)
	}
}

func TestNotification_NonPaymentTypes(t *testing.T) {
	for _, typ := range []string{"plan", "subscription", "invoice", "point_integration_wh", "topic_chargebacks_wh"} {
		n, err := ParseNotification([]byte(fmt.Sprintf(`{"type":%q,"data":{"id":"1"}}`, typ)), nil)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", typ, err)
		}
		if n.IsPayment() {
			t.Errorf("%q must not be treated as a payment notification", typ)
		}
	}
}

func TestNotification_NilSafety(t *testing.T) {
	var n *Notification
	if n.ResourceID() != "" || n.NormalizedType() != "" || n.IsPayment() {
		t.Fatal("nil notification accessors must be safe and empty")
	}
}

func TestToWebhookEvent_MapsFetchedPayment(t *testing.T) {
	p := &Payment{
		ID:                1234567890,
		Status:            StatusApproved,
		StatusDetail:      DetailAccredited,
		ExternalReference: "inv:abc",
		PaymentMethodID:   PaymentMethodPix,
		TransactionAmount: 49.9,
	}

	event, ok := ToWebhookEvent("notif-1", p)
	if !ok {
		t.Fatal("expected an actionable event")
	}
	if event.Event != payment.EventPaymentReceived {
		t.Fatalf("event: got %q", event.Event)
	}
	if event.Provider != payment.ProviderMercadoPago {
		t.Fatalf("provider: got %q", event.Provider)
	}
	if event.Payment.ID != "1234567890" {
		t.Fatalf("charge id: got %q", event.Payment.ID)
	}
	if event.Payment.ExternalReference != "inv:abc" {
		t.Fatalf("external reference: got %q", event.Payment.ExternalReference)
	}
	if event.Payment.Value != 49.9 {
		t.Fatalf("value: got %v", event.Payment.Value)
	}
	// BillingType feeds the confirmation email's "payment method" line, which formats
	// the canonical spelling.
	if event.Payment.BillingType != string(payment.MethodPix) {
		t.Fatalf("billing type: got %q", event.Payment.BillingType)
	}
}

func TestToWebhookEvent_NoActionableState(t *testing.T) {
	if _, ok := ToWebhookEvent("n", nil); ok {
		t.Fatal("a nil payment must not produce an event")
	}
	if _, ok := ToWebhookEvent("n", &Payment{ID: 1, Status: "some_future_status"}); ok {
		t.Fatal("an unknown status must not produce an event")
	}
}

// TestVerifySignatureAny_MatchesBodyIDWhenQueryAbsent reproduces the production
// symptom: Mercado Pago signed over the id it put in the BODY, and sent no data.id
// query parameter. Verifying against the query alone rejected an authentic
// notification, silently losing a paid invoice.
func TestVerifySignatureAny_MatchesBodyIDWhenQueryAbsent(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "1234567890", "req-abc", ts)

	// Query id is empty; the body id is the one that was signed.
	matched, err := VerifySignatureAny(header, "req-abc", []string{"", "1234567890"}, testSecret, 0, time.Now())
	if err != nil {
		t.Fatalf("expected the body id candidate to verify, got %v", err)
	}
	if matched != "1234567890" {
		t.Fatalf("expected the matched id to be reported, got %q", matched)
	}
}

func TestVerifySignatureAny_ReportsWhichCandidateMatched(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "999", "req", ts)

	matched, err := VerifySignatureAny(header, "req", []string{"111", "999", "222"}, testSecret, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched != "999" {
		t.Fatalf("wrong candidate reported: %q", matched)
	}
}

// TestVerifySignatureAny_ExtraCandidatesDoNotWeakenTheCheck: offering more candidates
// must never let an unsigned request through. Each candidate is a full HMAC over the
// same secret, so without the secret nothing passes regardless of how many are tried.
func TestVerifySignatureAny_ExtraCandidatesDoNotWeakenTheCheck(t *testing.T) {
	ts := time.Now().UnixMilli()
	forged := signatureHeader("attacker-secret", "1", "req", ts)

	many := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		many = append(many, strconv.Itoa(i))
	}
	if _, err := VerifySignatureAny(forged, "req", many, testSecret, 0, time.Now()); err == nil {
		t.Fatal("a forged signature passed despite the correct secret being required")
	}
}

func TestVerifySignatureAny_EmptyCandidateListFallsBackToNoID(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader(testSecret, "", "req", ts)

	matched, err := VerifySignatureAny(header, "req", nil, testSecret, 0, time.Now())
	if err != nil {
		t.Fatalf("expected a ts-only manifest to verify, got %v", err)
	}
	if matched != "" {
		t.Fatalf("expected an empty matched id, got %q", matched)
	}
}

func TestVerifySignatureAny_StillFailsClosedWithoutSecret(t *testing.T) {
	ts := time.Now().UnixMilli()
	header := signatureHeader("", "1", "req", ts)
	if _, err := VerifySignatureAny(header, "req", []string{"1"}, "", 0, time.Now()); err == nil {
		t.Fatal("an empty secret must reject even a self-consistent signature")
	}
}
