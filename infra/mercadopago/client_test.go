package mercadopago

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestClient(srv *httptest.Server, opts ...Option) Client {
	all := append([]Option{WithHTTPClient(srv.Client())}, opts...)
	return NewClient("TEST-token", srv.URL, all...)
}

type capturedRequest struct {
	method         string
	path           string
	authorization  string
	idempotencyKey string
	contentType    string
	body           []byte
}

func newRecordingServer(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.authorization = r.Header.Get("Authorization")
		captured.idempotencyKey = r.Header.Get("X-Idempotency-Key")
		captured.contentType = r.Header.Get("Content-Type")
		captured.body = raw
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

const pixPaymentBody = `{
  "id": 1234567890,
  "status": "pending",
  "status_detail": "pending_waiting_transfer",
  "external_reference": "inv:abc",
  "payment_method_id": "pix",
  "payment_type_id": "bank_transfer",
  "transaction_amount": 49.9,
  "currency_id": "BRL",
  "live_mode": false,
  "date_of_expiration": "2026-09-03T23:59:59.000-03:00",
  "point_of_interaction": {
    "type": "OPENPLATFORM",
    "transaction_data": {
      "qr_code": "00020126580014BR.GOV.BCB.PIX",
      "qr_code_base64": "aVZCT1J3MEtHZ29B",
      "ticket_url": "https://www.mercadopago.com.br/payments/1234567890/ticket"
    }
  },
  "transaction_details": {"external_resource_url": ""}
}`

func TestCreatePayment_SendsExpectedRequest(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusCreated, pixPaymentBody)
	c := newTestClient(srv, WithNotificationURL("https://vozko.test/webhooks/mercadopago"))

	got, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		TransactionAmount: 49.9,
		PaymentMethodID:   PaymentMethodPix,
		ExternalReference: "inv:abc",
		Payer: PayerRequest{
			Email:          "buyer@example.com",
			Identification: &Identification{Type: IdentificationCPF, Number: "11144477735"},
		},
	}, "idem-key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.method != http.MethodPost || captured.path != "/v1/payments" {
		t.Fatalf("expected POST /v1/payments, got %s %s", captured.method, captured.path)
	}
	if captured.authorization != "Bearer TEST-token" {
		t.Fatalf("bad Authorization header: %q", captured.authorization)
	}
	if captured.idempotencyKey != NormalizeIdempotencyKey("idem-key-1") {
		t.Fatalf("idempotency key not derived from the caller key, got %q", captured.idempotencyKey)
	}
	if _, err := uuid.Parse(captured.idempotencyKey); err != nil {
		t.Fatalf("X-Idempotency-Key must be a UUID, got %q", captured.idempotencyKey)
	}
	if captured.contentType != "application/json" {
		t.Fatalf("bad Content-Type: %q", captured.contentType)
	}

	var sent CreatePaymentRequest
	if err := json.Unmarshal(captured.body, &sent); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if sent.NotificationURL != "https://vozko.test/webhooks/mercadopago" {
		t.Fatalf("notification_url not defaulted, got %q", sent.NotificationURL)
	}

	if got.ID != 1234567890 {
		t.Fatalf("expected id 1234567890, got %d", got.ID)
	}
	if got.PointOfInteraction.TransactionData.QRCode == "" || got.PointOfInteraction.TransactionData.QRCodeBase64 == "" {
		t.Fatalf("PIX payload not decoded: %+v", got.PointOfInteraction)
	}
	if got.DateOfExpiration == nil {
		t.Fatal("expected date_of_expiration to decode")
	}
}

func TestCreatePayment_GeneratesIdempotencyKeyWhenAbsent(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusCreated, pixPaymentBody)
	c := newTestClient(srv)

	if _, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		TransactionAmount: 10, PaymentMethodID: PaymentMethodPix,
	}, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.idempotencyKey == "" {
		t.Fatal("expected a generated X-Idempotency-Key, got none")
	}
}

func TestCreatePayment_ExplicitNotificationURLWins(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusCreated, pixPaymentBody)
	c := newTestClient(srv, WithNotificationURL("https://default.test/hook"))

	if _, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		TransactionAmount: 10,
		PaymentMethodID:   PaymentMethodPix,
		NotificationURL:   "https://explicit.test/hook",
	}, "k"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sent CreatePaymentRequest
	_ = json.Unmarshal(captured.body, &sent)
	if sent.NotificationURL != "https://explicit.test/hook" {
		t.Fatalf("explicit notification_url overwritten: %q", sent.NotificationURL)
	}
}

func TestCreatePayment_MissingIDIsAnError(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusCreated, `{"status":"pending"}`)
	c := newTestClient(srv)
	if _, err := c.CreatePayment(context.Background(), CreatePaymentRequest{TransactionAmount: 1}, "k"); err == nil {
		t.Fatal("expected an error when the response carries no id")
	}
}

func TestCreatePayment_APIErrorIsStructured(t *testing.T) {
	body := `{"message":"payer.identification.number must be a valid CPF","error":"bad_request","status":400,"cause":[{"code":324,"description":"invalid parameter"}]}`
	srv, _ := newRecordingServer(t, http.StatusBadRequest, body)
	c := newTestClient(srv)

	_, err := c.CreatePayment(context.Background(), CreatePaymentRequest{TransactionAmount: 1}, "k")
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("expected *ResponseError, got %T: %v", err, err)
	}
	if respErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", respErr.StatusCode)
	}
	if !strings.Contains(respErr.Message, "valid CPF") {
		t.Fatalf("message not extracted: %q", respErr.Message)
	}
	if respErr.ErrorCode != "bad_request" || len(respErr.Causes) != 1 {
		t.Fatalf("error envelope not parsed: %+v", respErr)
	}
	if respErr.Retryable() {
		t.Fatal("a 400 must not be reported as retryable")
	}
}

func TestResponseError_UnwrapsToSentinels(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, ErrNotFound},
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
	}
	for _, c := range cases {
		srv, _ := newRecordingServer(t, c.status, `{"message":"nope"}`)
		client := newTestClient(srv)
		_, err := client.GetPayment(context.Background(), "123")
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: expected %v, got %v", c.status, c.want, err)
		}
	}
}

func TestResponseError_Retryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},
		{http.StatusLocked, true},
		{http.StatusFailedDependency, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
	}
	for _, c := range cases {
		e := &ResponseError{StatusCode: c.status}
		if got := e.Retryable(); got != c.want {
			t.Errorf("status %d: Retryable()=%v want %v", c.status, got, c.want)
		}
	}
}

func TestResponseError_FallsBackToRawBody(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusInternalServerError, "upstream exploded")
	c := newTestClient(srv)
	_, err := c.GetPayment(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("expected the raw body in the message, got %v", err)
	}
}

func TestGetPayment_RejectsNonNumericIDBeforeAnyRequest(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	for _, bad := range []string{"", "  ", "abc", "../../v1/users", "12 34", "12a"} {
		_, err := c.GetPayment(context.Background(), bad)
		if !errors.Is(err, ErrInvalidPaymentID) {
			t.Errorf("GetPayment(%q): expected ErrInvalidPaymentID, got %v", bad, err)
		}
	}
	if reached {
		t.Fatal("a malformed id must never reach the network")
	}
}

func TestGetPayment_Success(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusOK, pixPaymentBody)
	c := newTestClient(srv)

	got, err := c.GetPayment(context.Background(), "1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.method != http.MethodGet || captured.path != "/v1/payments/1234567890" {
		t.Fatalf("expected GET /v1/payments/1234567890, got %s %s", captured.method, captured.path)
	}
	if captured.idempotencyKey != "" {
		t.Fatal("reads must not carry an idempotency key")
	}
	if got.ExternalReference != "inv:abc" {
		t.Fatalf("external_reference not decoded: %q", got.ExternalReference)
	}
}

func TestRefundPayment_FullRefundSendsNoAmount(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusCreated, `{"id":9,"payment_id":1234567890,"amount":49.9,"status":"approved"}`)
	c := newTestClient(srv)

	got, err := c.RefundPayment(context.Background(), "1234567890", 0, "idem-refund")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.path != "/v1/payments/1234567890/refunds" || captured.method != http.MethodPost {
		t.Fatalf("unexpected request: %s %s", captured.method, captured.path)
	}
	if strings.Contains(string(captured.body), "amount") {
		t.Fatalf("full refund must not send an amount, body was %s", captured.body)
	}
	if got.ID != 9 {
		t.Fatalf("refund not decoded: %+v", got)
	}
}

func TestRefundPayment_PartialSendsAmount(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusCreated, `{"id":10,"amount":10.5,"status":"approved"}`)
	c := newTestClient(srv)

	if _, err := c.RefundPayment(context.Background(), "1234567890", 10.5, "k"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sent createRefundRequest
	if err := json.Unmarshal(captured.body, &sent); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if sent.Amount != 10.5 {
		t.Fatalf("expected amount 10.5, got %v", sent.Amount)
	}
}

func TestRefundPayment_RejectsBadID(t *testing.T) {
	c := NewClient("t", "http://unused")
	if _, err := c.RefundPayment(context.Background(), "nope", 1, "k"); !errors.Is(err, ErrInvalidPaymentID) {
		t.Fatalf("expected ErrInvalidPaymentID, got %v", err)
	}
}

func TestCancelPayment_SendsCancelledStatus(t *testing.T) {
	srv, captured := newRecordingServer(t, http.StatusOK, `{"id":1234567890,"status":"cancelled","status_detail":"by_collector"}`)
	c := newTestClient(srv)

	got, err := c.CancelPayment(context.Background(), "1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.method != http.MethodPut || captured.path != "/v1/payments/1234567890" {
		t.Fatalf("expected PUT /v1/payments/1234567890, got %s %s", captured.method, captured.path)
	}
	var sent updatePaymentRequest
	_ = json.Unmarshal(captured.body, &sent)
	if sent.Status != StatusCancelled {
		t.Fatalf("expected status cancelled in body, got %q", sent.Status)
	}
	if got.Status != StatusCancelled {
		t.Fatalf("response not decoded: %+v", got)
	}
}

func TestClient_MissingAccessTokenFailsBeforeRequest(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	t.Cleanup(srv.Close)
	c := NewClient("   ", srv.URL, WithHTTPClient(srv.Client()))

	if _, err := c.GetPayment(context.Background(), "1"); !errors.Is(err, ErrMissingAccessToken) {
		t.Fatalf("expected ErrMissingAccessToken, got %v", err)
	}
	if reached {
		t.Fatal("must not call the API without a token")
	}
}

func TestClient_TransportErrorIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	srv.Close()
	c := NewClient("t", srv.URL, WithHTTPClient(client))

	if _, err := c.GetPayment(context.Background(), "1"); err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestClient_ContextCancellationPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetPayment(ctx, "1"); err == nil {
		t.Fatal("expected the cancelled context to surface as an error")
	}
}

func TestClient_MalformedJSONResponse(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusOK, "{not json")
	c := newTestClient(srv)
	_, err := c.GetPayment(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected a decode error, got %v", err)
	}
}

func TestNewClient_DefaultsBaseURLAndTrimsTrailingSlash(t *testing.T) {
	c := NewClient("t", "").(*client)
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("expected default base URL, got %q", c.baseURL)
	}
	c2 := NewClient("t", "https://proxy.test/").(*client)
	if c2.baseURL != "https://proxy.test" {
		t.Fatalf("trailing slash not trimmed: %q", c2.baseURL)
	}
}

func TestFormatExpiration(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	if got := FormatExpiration(time.Time{}, now, true); got != "" {
		t.Fatalf("zero time must render empty, got %q", got)
	}

	within := now.Add(48 * time.Hour)
	got := FormatExpiration(within, now, true)
	if got != "2026-09-02T12:00:00.000+00:00" {
		t.Fatalf("unexpected layout: %q", got)
	}
	if _, err := time.Parse(ExpirationLayout, got); err != nil {
		t.Fatalf("output does not round-trip: %v", err)
	}

	tooSoon := FormatExpiration(now.Add(time.Minute), now, true)
	parsed, err := time.Parse(ExpirationLayout, tooSoon)
	if err != nil {
		t.Fatalf("bad output: %v", err)
	}
	if !parsed.Equal(now.Add(MinPixExpiry)) {
		t.Fatalf("expected clamp to the 30-minute floor, got %v", parsed)
	}

	tooLate := FormatExpiration(now.AddDate(0, 6, 0), now, true)
	parsedLate, _ := time.Parse(ExpirationLayout, tooLate)
	if !parsedLate.Equal(now.Add(MaxPixExpiry)) {
		t.Fatalf("expected clamp to the 30-day ceiling, got %v", parsedLate)
	}

	unclamped := FormatExpiration(now.Add(time.Minute), now, false)
	parsedUnclamped, _ := time.Parse(ExpirationLayout, unclamped)
	if !parsedUnclamped.Equal(now.Add(time.Minute)) {
		t.Fatalf("unclamped value was modified: %v", parsedUnclamped)
	}
}

func TestIdentificationTypeFor(t *testing.T) {
	cases := []struct {
		doc  string
		want string
	}{
		{"111.444.777-35", IdentificationCPF},
		{"11144477735", IdentificationCPF},
		{"12.345.678/0001-95", IdentificationCNPJ},
		{"12345678000195", IdentificationCNPJ},
		{"", IdentificationCPF},
		{"garbage", IdentificationCPF},
	}
	for _, c := range cases {
		if got := IdentificationTypeFor(c.doc); got != c.want {
			t.Errorf("IdentificationTypeFor(%q)=%q want %q", c.doc, got, c.want)
		}
	}
}

func TestOnlyDigits(t *testing.T) {
	if got := OnlyDigits("111.444.777-35"); got != "11144477735" {
		t.Fatalf("got %q", got)
	}
	if got := OnlyDigits("abc"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := OnlyDigits(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct {
		in          string
		first, last string
	}{
		{"Maria Silva", "Maria", "Silva"},
		{"Maria da Silva Souza", "Maria", "da Silva Souza"},
		{"Prince", "Prince", ""},
		{"   ", "", ""},
		{"", "", ""},
		{"  Ana   Paula  ", "Ana", "Paula"},
	}
	for _, c := range cases {
		first, last := SplitName(c.in)
		if first != c.first || last != c.last {
			t.Errorf("SplitName(%q)=(%q,%q) want (%q,%q)", c.in, first, last, c.first, c.last)
		}
	}
}

func TestFormatPaymentID(t *testing.T) {
	if got := FormatPaymentID(1234567890); got != "1234567890" {
		t.Fatalf("got %q", got)
	}
	if got := FormatPaymentID(0); got != "0" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeIdempotencyKey(t *testing.T) {
	first := NormalizeIdempotencyKey("inv:1a2b3c")
	second := NormalizeIdempotencyKey("inv:1a2b3c")
	if first != second {
		t.Fatalf("derivation is not deterministic: %q vs %q", first, second)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("derived key is not a UUID: %q", first)
	}

	if NormalizeIdempotencyKey("inv:aaa") == NormalizeIdempotencyKey("inv:bbb") {
		t.Fatal("distinct keys collided")
	}

	existing := "6b3f2a1c-9d4e-4f8a-b7c2-1e5d0a3f6b91"
	if got := NormalizeIdempotencyKey(existing); got != existing {
		t.Fatalf("a UUID key must pass through, got %q", got)
	}

	a, b := NormalizeIdempotencyKey(""), NormalizeIdempotencyKey("   ")
	if a == b {
		t.Fatal("empty keys must produce distinct random UUIDs")
	}
	if _, err := uuid.Parse(a); err != nil {
		t.Fatalf("generated key is not a UUID: %q", a)
	}
}
