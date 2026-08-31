package mercadopago

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/quick"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Property tests. The table-driven tests elsewhere pin the documented examples; these
// assert the invariants that must hold for EVERY input, which is where an integration
// against someone else's API tends to break.

var quickConfig = &quick.Config{MaxCount: 500}

// --- FormatExpiration -------------------------------------------------------

// Property: a clamped expiration always lands inside Mercado Pago's accepted PIX
// window. Anything outside it is rejected outright, so this is the invariant that keeps
// an odd due date from failing the charge.
func TestProperty_FormatExpiration_AlwaysInsidePixWindow(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	f := func(offsetSeconds int64) bool {
		due := now.Add(time.Duration(offsetSeconds) * time.Second)
		out := FormatExpiration(due, now, true)
		parsed, err := time.Parse(ExpirationLayout, out)
		if err != nil {
			t.Logf("unparseable output %q for offset %d", out, offsetSeconds)
			return false
		}
		earliest := now.Add(MinPixExpiry).Add(-time.Millisecond)
		latest := now.Add(MaxPixExpiry).Add(time.Millisecond)
		return !parsed.Before(earliest) && !parsed.After(latest)
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: the rendered value always round-trips through the layout Mercado Pago
// documents. A layout mistake here fails every charge, so it is worth asserting over
// arbitrary instants rather than one example.
func TestProperty_FormatExpiration_RoundTrips(t *testing.T) {
	f := func(unixSeconds int64, offsetMinutes int16) bool {
		// Keep to a sane range: year 2000-2100, offsets within ±14h.
		sec := 946684800 + (unixSeconds%3_155_760_000+3_155_760_000)%3_155_760_000
		off := int(offsetMinutes%840) * 60
		due := time.Unix(sec, 0).In(time.FixedZone("test", off))

		out := FormatExpiration(due, time.Now(), false)
		parsed, err := time.Parse(ExpirationLayout, out)
		if err != nil {
			t.Logf("layout failed for %v: %v (out=%q)", due, err, out)
			return false
		}
		return parsed.Unix() == due.Unix()
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: clamping is idempotent. Re-clamping an already-clamped value must not drift
// it further, or a retry could walk the expiry outward on every attempt.
func TestProperty_FormatExpiration_ClampIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	f := func(offsetSeconds int64) bool {
		due := now.Add(time.Duration(offsetSeconds) * time.Second)
		once := FormatExpiration(due, now, true)
		parsed, err := time.Parse(ExpirationLayout, once)
		if err != nil {
			return false
		}
		twice := FormatExpiration(parsed, now, true)
		return once == twice
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: without clamping the caller's instant is preserved to the millisecond.
func TestProperty_FormatExpiration_UnclampedPreservesInstant(t *testing.T) {
	f := func(unixMillis int64) bool {
		ms := 946684800000 + (unixMillis%3_155_760_000_000+3_155_760_000_000)%3_155_760_000_000
		due := time.UnixMilli(ms).UTC()
		out := FormatExpiration(due, time.Now(), false)
		parsed, err := time.Parse(ExpirationLayout, out)
		if err != nil {
			return false
		}
		return parsed.UnixMilli() == due.UnixMilli()
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// --- NormalizeIdempotencyKey ------------------------------------------------

// Property: every non-empty caller key maps to a well-formed UUID, deterministically.
// Mercado Pago documents a UUID for this header, and the determinism is what makes a
// retried charge safe to repeat rather than double-charging.
func TestProperty_NormalizeIdempotencyKey_IsDeterministicUUID(t *testing.T) {
	f := func(raw string) bool {
		if strings.TrimSpace(raw) == "" {
			return true // empty is deliberately random; covered separately
		}
		first := NormalizeIdempotencyKey(raw)
		if _, err := uuid.Parse(first); err != nil {
			t.Logf("not a UUID for %q: %q", raw, first)
			return false
		}
		return first == NormalizeIdempotencyKey(raw)
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: distinct logical keys do not collide. A collision would make two different
// charges share an idempotency key, and the second would silently return the first.
func TestProperty_NormalizeIdempotencyKey_NoCollisions(t *testing.T) {
	seen := make(map[string]string, 5000)
	for i := 0; i < 5000; i++ {
		key := fmt.Sprintf("inv:%s", uuid.NewString())
		derived := NormalizeIdempotencyKey(key)
		if prev, dup := seen[derived]; dup && prev != key {
			t.Fatalf("collision: %q and %q both derive %q", prev, key, derived)
		}
		seen[derived] = key
	}
}

// --- OnlyDigits / SplitName -------------------------------------------------

// Property: the result contains only digits, and only digits that were in the input, in
// order. Mercado Pago rejects a punctuated CPF, so nothing may survive but the digits.
func TestProperty_OnlyDigits(t *testing.T) {
	f := func(raw string) bool {
		out := OnlyDigits(raw)
		for _, r := range out {
			if r < '0' || r > '9' {
				return false
			}
		}
		// Idempotent, and equal to filtering the input directly.
		if OnlyDigits(out) != out {
			return false
		}
		var expected strings.Builder
		for _, r := range raw {
			if unicode.IsDigit(r) && r < 128 {
				expected.WriteRune(r)
			}
		}
		return out == expected.String()
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: the split name recombines to the whitespace-normalized original, and
// neither half carries stray padding. Mercado Pago validates these fields, so a stray
// space is a rejected charge.
func TestProperty_SplitName_Recombines(t *testing.T) {
	f := func(raw string) bool {
		first, last := SplitName(raw)
		if first != strings.TrimSpace(first) || last != strings.TrimSpace(last) {
			return false
		}
		recombined := strings.TrimSpace(first + " " + last)
		return recombined == strings.Join(strings.Fields(raw), " ")
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// --- Status and event mapping -----------------------------------------------

var (
	allStatuses = []string{
		StatusPending, StatusApproved, StatusAuthorized, StatusInProcess,
		StatusInMediation, StatusRejected, StatusCancelled, StatusRefunded,
		StatusChargedBack, "", "unknown_future_status",
	}
	allDetails = []string{
		DetailAccredited, DetailPartiallyRefunded, DetailExpired,
		DetailPendingWaitingTransfer, DetailPendingWaitingPayment,
		"by_collector", "by_payer", "cc_rejected_high_risk", "", "unknown_detail",
	}
)

// Property: no combination of status, detail and refunded amount ever produces
// PAYMENT_RECEIVED for money that is not actually settled and unrefunded. This is the
// invariant that stops a refunded or rejected charge from crediting saldo.
func TestProperty_MapEvent_NeverCreditsUnsettledMoney(t *testing.T) {
	amounts := []struct{ total, refunded float64 }{
		{100, 0}, {100, 0.01}, {100, 50}, {100, 100}, {100, 150}, {0, 0}, {0, 10},
	}

	for _, status := range allStatuses {
		for _, detail := range allDetails {
			for _, amt := range amounts {
				p := &Payment{
					Status: status, StatusDetail: detail,
					TransactionAmount: amt.total, TransactionAmountRefunded: amt.refunded,
				}
				event := MapEvent(p)
				if event != EventReceivedForTest {
					continue
				}
				// Only an approved, unrefunded payment may be reported as received.
				if !strings.EqualFold(strings.TrimSpace(status), StatusApproved) {
					t.Fatalf("status=%q detail=%q refunded=%v produced RECEIVED", status, detail, amt.refunded)
				}
				if amt.refunded > 0 {
					t.Fatalf("a payment with %v refunded produced RECEIVED (status=%q detail=%q)", amt.refunded, status, detail)
				}
				if strings.EqualFold(detail, DetailPartiallyRefunded) {
					t.Fatalf("a partially refunded payment produced RECEIVED (status=%q)", status)
				}
			}
		}
	}
}

// EventReceivedForTest mirrors the canonical constant without importing the domain
// package into this property helper's comparison, keeping the assertion above readable.
const EventReceivedForTest = "PAYMENT_RECEIVED"

// Property: mapping is total. Every combination returns without panicking, and every
// non-empty event is one the shared handler recognizes. An unrecognized string would be
// silently ignored downstream, which is exactly the kind of bug that loses a payment.
func TestProperty_MapEvent_IsTotalAndKnown(t *testing.T) {
	known := map[string]bool{
		"PAYMENT_CREATED": true, "PAYMENT_RECEIVED": true, "PAYMENT_OVERDUE": true,
		"PAYMENT_REFUNDED": true, "PAYMENT_PARTIALLY_REFUNDED": true,
		"PAYMENT_DELETED": true, "PAYMENT_AUTHORIZED": true, "PAYMENT_REJECTED": true,
		"PAYMENT_CHARGEBACK": true, "PAYMENT_IN_ANALYSIS": true,
	}
	for _, status := range allStatuses {
		for _, detail := range allDetails {
			event := MapEvent(&Payment{Status: status, StatusDetail: detail, TransactionAmount: 10})
			if event != "" && !known[event] {
				t.Fatalf("status=%q detail=%q produced unknown event %q", status, detail, event)
			}
		}
	}
}

// Property: mapping ignores case and surrounding whitespace, so a provider that starts
// echoing " Approved " cannot silently stop crediting payments.
func TestProperty_Mapping_IgnoresCaseAndPadding(t *testing.T) {
	for _, status := range allStatuses {
		for _, detail := range allDetails {
			base := MapEvent(&Payment{Status: status, StatusDetail: detail, TransactionAmount: 10})
			noisy := MapEvent(&Payment{
				Status:            "  " + strings.ToUpper(status) + " ",
				StatusDetail:      " " + strings.ToUpper(detail) + "  ",
				TransactionAmount: 10,
			})
			if base != noisy {
				t.Fatalf("status=%q detail=%q: %q vs padded/upper %q", status, detail, base, noisy)
			}
			if MapStatus(status) != MapStatus("  "+strings.ToUpper(status)+"  ") {
				t.Fatalf("MapStatus not case/padding insensitive for %q", status)
			}
		}
	}
}

// Property: a status this integration maps to "received" must never simultaneously map
// to a cancelling event, and vice versa. The two mappings feed different tables and
// must not contradict each other.
func TestProperty_MapStatusAndMapEventAgree(t *testing.T) {
	for _, status := range allStatuses {
		event := MapEvent(&Payment{Status: status, TransactionAmount: 10})
		mapped := MapStatus(status)

		switch event {
		case "PAYMENT_RECEIVED":
			if string(mapped) != "RECEIVED" {
				t.Fatalf("status %q: event RECEIVED but status %q", status, mapped)
			}
		case "PAYMENT_REJECTED", "PAYMENT_DELETED":
			if string(mapped) != "CANCELLED" {
				t.Fatalf("status %q: terminal event %q but status %q", status, event, mapped)
			}
		case "PAYMENT_REFUNDED", "PAYMENT_CHARGEBACK":
			if string(mapped) != "REFUNDED" {
				t.Fatalf("status %q: event %q but status %q", status, event, mapped)
			}
		}
	}
}

// --- Signature verification -------------------------------------------------

// Property: any correctly signed request verifies, for arbitrary ids and request ids.
func TestProperty_VerifySignature_RoundTrip(t *testing.T) {
	f := func(dataID, requestID string, tsOffset int32) bool {
		// The manifest is built from trimmed values, so compare on those.
		dataID = strings.TrimSpace(dataID)
		requestID = strings.TrimSpace(requestID)
		ts := time.Now().UnixMilli() + int64(tsOffset)

		header := signatureHeader(testSecret, strings.ToLower(dataID), requestID, ts)
		err := VerifySignature(header, requestID, dataID, testSecret, 0, time.Now())
		if err != nil {
			t.Logf("failed for dataID=%q requestID=%q: %v", dataID, requestID, err)
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: mutating ANY signed component invalidates the signature. This is the
// security core: it is what stops a valid notification being replayed against a
// different payment.
func TestProperty_VerifySignature_AnyMutationFails(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))

	for i := 0; i < 500; i++ {
		dataID := strconv.FormatInt(rng.Int63(), 10)
		requestID := uuid.NewString()
		ts := time.Now().UnixMilli()
		header := signatureHeader(testSecret, dataID, requestID, ts)

		// Sanity: the unmutated form must verify, or the mutations prove nothing.
		if err := VerifySignature(header, requestID, dataID, testSecret, 0, time.Now()); err != nil {
			t.Fatalf("baseline failed: %v", err)
		}

		mutations := []struct {
			name                        string
			hdr, gotReqID, gotDataID, s string
		}{
			{"data id", header, requestID, dataID + "0", testSecret},
			{"request id", header, requestID + "x", dataID, testSecret},
			{"secret", header, requestID, dataID, testSecret + "x"},
			{"timestamp", signatureHeaderRawTS(header, ts+1), requestID, dataID, testSecret},
			{"hash", flipLastHexDigit(header), requestID, dataID, testSecret},
		}
		for _, m := range mutations {
			if err := VerifySignature(m.hdr, m.gotReqID, m.gotDataID, m.s, 0, time.Now()); err == nil {
				t.Fatalf("mutation %q was accepted (dataID=%s)", m.name, dataID)
			}
		}
	}
}

// signatureHeaderRawTS swaps only the advertised ts, leaving the hash untouched.
func signatureHeaderRawTS(header string, newTS int64) string {
	_, rest, ok := strings.Cut(header, ",")
	if !ok {
		return header
	}
	return "ts=" + strconv.FormatInt(newTS, 10) + "," + rest
}

func flipLastHexDigit(header string) string {
	if header == "" {
		return header
	}
	last := header[len(header)-1]
	replacement := byte('a')
	if last == 'a' {
		replacement = 'b'
	}
	return header[:len(header)-1] + string(replacement)
}

// Property: verification never panics, whatever arrives in the headers. This endpoint is
// public, so any input at all can reach it.
func TestProperty_VerifySignature_NeverPanics(t *testing.T) {
	f := func(sig, reqID, dataID, secret string) bool {
		_ = VerifySignature(sig, reqID, dataID, secret, 0, time.Now())
		_ = VerifySignature(sig, reqID, dataID, secret, time.Minute, time.Now())
		return true
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// Property: an empty secret rejects every input, always. This is the fail-closed
// guarantee that keeps a misconfigured deployment from being an open endpoint.
func TestProperty_VerifySignature_EmptySecretAlwaysRejects(t *testing.T) {
	f := func(sig, reqID, dataID string) bool {
		return VerifySignature(sig, reqID, dataID, "", 0, time.Now()) != nil
	}
	if err := quick.Check(f, quickConfig); err != nil {
		t.Fatal(err)
	}
}

// --- Fuzz targets -----------------------------------------------------------
//
// These run their seed corpus as ordinary unit tests, and can be fuzzed on demand with
// `go test -run XXX -fuzz FuzzParseNotification ./infra/mercadopago`.

func FuzzParseNotification(f *testing.F) {
	f.Add([]byte(`{"type":"payment","action":"payment.updated","data":{"id":"123"}}`), "", "")
	f.Add([]byte(`{"topic":"payment","resource":"https://api.mercadolibre.com/collections/notifications/1"}`), "", "")
	f.Add([]byte(``), "payment", "999")
	f.Add([]byte(`{`), "", "")
	f.Add([]byte(`{"data":{"id":null}}`), "", "")
	f.Add([]byte(`{"id":"not-a-number"}`), "", "")

	f.Fuzz(func(t *testing.T, body []byte, topic, id string) {
		q := url.Values{}
		if topic != "" {
			q.Set("topic", topic)
		}
		if id != "" {
			q.Set("id", id)
		}

		n, err := ParseNotification(body, q)
		if err != nil {
			// Every failure must be one of the two terminal kinds the consumer knows
			// how to drop, never an untyped surprise.
			if !errors.Is(err, ErrNotificationMissingResourceID) && !strings.Contains(err.Error(), "invalid notification body") {
				t.Fatalf("unexpected error shape: %v", err)
			}
			return
		}
		// Success must always yield something the consumer can act on.
		if n.ResourceID() == "" {
			t.Fatal("ParseNotification succeeded with no resource id")
		}
		// Accessors must be safe on whatever was parsed.
		_ = n.IsPayment()
		_ = n.NormalizedType()
	})
}

func FuzzVerifySignature(f *testing.F) {
	valid := func() string {
		ts := time.Now().UnixMilli()
		manifest := "id:1;request-id:r;ts:" + strconv.FormatInt(ts, 10) + ";"
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write([]byte(manifest))
		return "ts=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	}()

	f.Add(valid, "r", "1")
	f.Add("ts=1,v1=deadbeef", "r", "1")
	f.Add("garbage", "", "")
	f.Add("ts=,v1=", "", "")
	f.Add("", "", "")
	f.Add("ts=1,v1=", "x", "y")
	f.Add(strings.Repeat("ts=1,", 100), "", "")

	f.Fuzz(func(t *testing.T, sig, reqID, dataID string) {
		err := VerifySignature(sig, reqID, dataID, testSecret, 0, time.Now())
		if err == nil {
			// The only way to pass is to hold the secret, so a passing input must
			// reproduce under a recomputation with the same inputs.
			if again := VerifySignature(sig, reqID, dataID, testSecret, 0, time.Now()); again != nil {
				t.Fatal("verification is not deterministic")
			}
			return
		}
		var sigErr *SignatureError
		if !errors.As(err, &sigErr) {
			t.Fatalf("rejection must be a *SignatureError, got %T: %v", err, err)
		}
		if !errors.Is(err, ErrInvalidSignature) {
			t.Fatal("rejection must satisfy errors.Is(ErrInvalidSignature)")
		}
	})
}

func FuzzNormalizeIdempotencyKey(f *testing.F) {
	f.Add("inv:abc")
	f.Add("")
	f.Add("6b3f2a1c-9d4e-4f8a-b7c2-1e5d0a3f6b91")
	f.Add(strings.Repeat("x", 1000))

	f.Fuzz(func(t *testing.T, key string) {
		got := NormalizeIdempotencyKey(key)
		if _, err := uuid.Parse(got); err != nil {
			t.Fatalf("NormalizeIdempotencyKey(%q) = %q, not a UUID", key, got)
		}
		if strings.TrimSpace(key) != "" && got != NormalizeIdempotencyKey(key) {
			t.Fatalf("not deterministic for %q", key)
		}
	})
}

func FuzzOnlyDigits(f *testing.F) {
	f.Add("111.444.777-35")
	f.Add("")
	f.Add("abc")
	f.Add("１２３") // full-width digits must NOT survive: Mercado Pago wants ASCII

	f.Fuzz(func(t *testing.T, raw string) {
		out := OnlyDigits(raw)
		for _, r := range out {
			if r < '0' || r > '9' {
				t.Fatalf("OnlyDigits(%q) = %q contains a non-ASCII-digit rune %q", raw, out, r)
			}
		}
		if OnlyDigits(out) != out {
			t.Fatalf("OnlyDigits is not idempotent for %q", raw)
		}
	})
}
