package mercadopago

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefaultBaseURL is Mercado Pago's single global API host. Unlike Asaas there is no
// separate sandbox host: test versus production is decided by which access token is
// used, and the resulting payment carries live_mode=false.
const DefaultBaseURL = "https://api.mercadopago.com"

const defaultTimeout = 30 * time.Second

// ExpirationLayout is the only date layout Mercado Pago accepts for
// date_of_expiration: ISO-8601 with milliseconds and an explicit UTC offset.
const ExpirationLayout = "2006-01-02T15:04:05.000-07:00"

// PIX expiry bounds enforced by Mercado Pago. A request outside them is rejected, so
// the client clamps instead of letting a caller's due date fail the charge outright.
const (
	MinPixExpiry = 30 * time.Minute
	MaxPixExpiry = 30 * 24 * time.Hour
)

// paymentIDPattern guards path interpolation. Mercado Pago payment ids are numeric, so
// anything else is a bug or an injection attempt and never reaches the network.
var paymentIDPattern = regexp.MustCompile(`^[0-9]+$`)

// Client is the Mercado Pago Payments API surface this integration needs.
type Client interface {
	CreatePayment(ctx context.Context, req CreatePaymentRequest, idempotencyKey string) (*Payment, error)
	GetPayment(ctx context.Context, paymentID string) (*Payment, error)
	RefundPayment(ctx context.Context, paymentID string, amount float64, idempotencyKey string) (*Refund, error)
	CancelPayment(ctx context.Context, paymentID string) (*Payment, error)
}

// HTTPClient is the http.Client subset used, so tests can inject a transport.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type client struct {
	accessToken     string
	baseURL         string
	notificationURL string
	http            HTTPClient
}

// Option configures a Client at construction.
type Option func(*client)

// WithHTTPClient overrides the HTTP client (tests, custom transports, proxies).
func WithHTTPClient(h HTTPClient) Option {
	return func(c *client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithNotificationURL sets the per-payment notification_url. Setting it per payment
// rather than relying on the dashboard-wide URL is what guarantees the data.id query
// parameter that the webhook signature is computed over.
func WithNotificationURL(u string) Option {
	return func(c *client) { c.notificationURL = strings.TrimSpace(u) }
}

// NewClient builds a Mercado Pago API client. An empty baseURL uses DefaultBaseURL.
func NewClient(accessToken, baseURL string, opts ...Option) Client {
	c := &client{
		accessToken: strings.TrimSpace(accessToken),
		baseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:        &http.Client{Timeout: defaultTimeout},
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

var (
	// ErrNotFound is returned for HTTP 404.
	ErrNotFound = errors.New("mercadopago: resource not found")
	// ErrUnauthorized is returned for HTTP 401/403, which in practice always means a
	// bad or wrong-environment access token.
	ErrUnauthorized = errors.New("mercadopago: unauthorized")
	// ErrInvalidPaymentID is returned before any request when the id is not numeric.
	ErrInvalidPaymentID = errors.New("mercadopago: invalid payment id")
	// ErrMissingAccessToken is returned when the client was built without a token.
	ErrMissingAccessToken = errors.New("mercadopago: access token is not configured")
)

// ResponseError carries a non-2xx API response, with the parsed error envelope when
// Mercado Pago returned one.
type ResponseError struct {
	StatusCode int
	Message    string
	ErrorCode  string
	Causes     []ErrorCause
	Body       string
}

func (e *ResponseError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	// The cause list is where Mercado Pago puts the actionable detail; message alone is
	// often a generic wrapper ("fill and validate error list: communication_error").
	// Folding the causes into the error text is what makes a failed charge diagnosable
	// from a log line instead of requiring a reproduction.
	if detail := formatCauses(e.Causes); detail != "" {
		msg += " [" + detail + "]"
	}
	if hint := e.Hint(); hint != "" {
		msg += " — likely cause: " + hint
	}
	return fmt.Sprintf("mercadopago: request failed with status %d: %s", e.StatusCode, msg)
}

func formatCauses(causes []ErrorCause) string {
	parts := make([]string, 0, len(causes))
	for _, c := range causes {
		switch {
		case c.Code != nil && c.Description != "":
			parts = append(parts, fmt.Sprintf("%v: %s", c.Code, c.Description))
		case c.Description != "":
			parts = append(parts, c.Description)
		case c.Code != nil:
			parts = append(parts, fmt.Sprintf("%v", c.Code))
		}
	}
	return strings.Join(parts, "; ")
}

// knownFailureHints maps a substring of a Mercado Pago error onto an explanation an
// operator can act on. Mercado Pago's most common charge failures are reported through
// opaque wrappers, so without this the log says only that something went wrong.
var knownFailureHints = []struct {
	match string
	hint  string
}{
	{"without key enabled for qr render",
		"the collector Mercado Pago account has no PIX key registered; add one in the account before issuing PIX charges"},
	{"collector user without key",
		"the collector Mercado Pago account has no PIX key registered; add one in the account before issuing PIX charges"},
	{"cannot operate between different countries",
		"the access token's country does not match the charge; use a Brazilian (MLB) account"},
	{"payer and collector",
		"the payer email belongs to the same account as the access token; Mercado Pago refuses to let an account pay itself, so a manual test needs a different payer email (a test user in sandbox)"},
	{"cannot pay yourself",
		"the payer email belongs to the same account as the access token; use a different payer email (a test user in sandbox)"},
	{"invalid users involved",
		"payer and collector belong to the same account or to mismatched environments; a TEST- token must be paired with a test-user payer email"},
	{"communication_error",
		"Mercado Pago wrapped a downstream rejection. In practice this is almost always (1) no PIX key registered on the collector account, (2) a payer email that belongs to the collector account itself, or (3) a TEST- access token paired with a real payer email"},
}

// Hint returns a human explanation for the most common causes, or empty when the API
// error is already self-explanatory.
func (e *ResponseError) Hint() string {
	haystack := strings.ToLower(e.Message + " " + e.Body + " " + formatCauses(e.Causes))
	for _, h := range knownFailureHints {
		if strings.Contains(haystack, h.match) {
			return h.hint
		}
	}
	return ""
}

// Unwrap maps the transport-level status onto the package's sentinel errors so callers
// can use errors.Is without inspecting status codes.
func (e *ResponseError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	}
	return nil
}

// Retryable reports whether repeating the request could plausibly succeed. 429 and 5xx
// are transient; Mercado Pago documents 423 (locked) and 424 (failed dependency) as
// retryable too.
func (e *ResponseError) Retryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests, http.StatusLocked, http.StatusFailedDependency:
		return true
	}
	return e.StatusCode >= 500
}

func validatePaymentID(id string) error {
	if !paymentIDPattern.MatchString(strings.TrimSpace(id)) {
		return fmt.Errorf("%w: %q", ErrInvalidPaymentID, id)
	}
	return nil
}

// do performs one API call. body may be nil; out may be nil to discard the response.
func (c *client) do(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	if c.accessToken == "" {
		return ErrMissingAccessToken
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mercadopago: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("mercadopago: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Mercado Pago requires X-Idempotency-Key on payment writes; sending one on every
	// write is what makes a queue redelivery or a client retry safe to repeat.
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mercadopago: %s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("mercadopago: read response: %w", err)
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return newResponseError(res.StatusCode, raw)
	}

	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("mercadopago: decode response: %w", err)
	}
	return nil
}

func newResponseError(status int, raw []byte) *ResponseError {
	respErr := &ResponseError{StatusCode: status, Body: string(raw)}
	var apiErr APIError
	if err := json.Unmarshal(raw, &apiErr); err == nil {
		respErr.Message = apiErr.Message
		respErr.ErrorCode = apiErr.Error
		respErr.Causes = apiErr.Cause
	}
	if respErr.Message == "" {
		respErr.Message = strings.TrimSpace(string(raw))
	}
	return respErr
}

func (c *client) CreatePayment(ctx context.Context, req CreatePaymentRequest, idempotencyKey string) (*Payment, error) {
	if req.NotificationURL == "" {
		req.NotificationURL = c.notificationURL
	}

	var out Payment
	if err := c.do(ctx, http.MethodPost, "/v1/payments", req, NormalizeIdempotencyKey(idempotencyKey), &out); err != nil {
		return nil, err
	}
	if out.ID == 0 {
		return nil, errors.New("mercadopago: create payment returned no id")
	}
	return &out, nil
}

func (c *client) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	if err := validatePaymentID(paymentID); err != nil {
		return nil, err
	}
	var out Payment
	if err := c.do(ctx, http.MethodGet, "/v1/payments/"+paymentID, nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RefundPayment refunds a payment. A non-positive amount requests a full refund, which
// Mercado Pago expects as an empty body rather than an explicit zero amount.
func (c *client) RefundPayment(ctx context.Context, paymentID string, amount float64, idempotencyKey string) (*Refund, error) {
	if err := validatePaymentID(paymentID); err != nil {
		return nil, err
	}
	idempotencyKey = NormalizeIdempotencyKey(idempotencyKey)

	var body any
	if amount > 0 {
		body = createRefundRequest{Amount: amount}
	} else {
		body = struct{}{}
	}

	var out Refund
	if err := c.do(ctx, http.MethodPost, "/v1/payments/"+paymentID+"/refunds", body, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) CancelPayment(ctx context.Context, paymentID string) (*Payment, error) {
	if err := validatePaymentID(paymentID); err != nil {
		return nil, err
	}
	var out Payment
	body := updatePaymentRequest{Status: StatusCancelled}
	if err := c.do(ctx, http.MethodPut, "/v1/payments/"+paymentID, body, uuid.NewString(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// idempotencyNamespace is a fixed UUID that scopes derived idempotency keys to this
// integration, so a caller key like "inv:<uuid>" always maps to the same UUID and never
// collides with an unrelated system's key.
var idempotencyNamespace = uuid.MustParse("6f9c1f4e-3a2b-5d7e-9c11-0b6d2f8a4c73")

// NormalizeIdempotencyKey turns a caller key into the UUID form Mercado Pago documents
// for X-Idempotency-Key.
//
// The keys this system produces are meaningful strings ("inv:<invoice id>"), which is
// what makes a retried charge safe to repeat. Mercado Pago, however, specifies a UUID
// v4 for this header, and a non-UUID value is not reliably honoured. Hashing the key
// into a deterministic v5 UUID keeps both properties: the same logical operation always
// yields the same header value, and that value is a well-formed UUID.
//
// A key that is already a UUID is passed through untouched, and an empty key gets a
// fresh random one (nothing to be idempotent about).
func NormalizeIdempotencyKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return uuid.NewString()
	}
	if _, err := uuid.Parse(key); err == nil {
		return key
	}
	return uuid.NewSHA1(idempotencyNamespace, []byte(key)).String()
}

// FormatExpiration renders t in the only layout Mercado Pago accepts, clamped into the
// PIX window when clamp is set. A due date outside that window would be rejected
// outright, so clamping keeps the charge issuable.
func FormatExpiration(t time.Time, now time.Time, clamp bool) string {
	if t.IsZero() {
		return ""
	}
	if clamp {
		if earliest := now.Add(MinPixExpiry); t.Before(earliest) {
			t = earliest
		}
		if latest := now.Add(MaxPixExpiry); t.After(latest) {
			t = latest
		}
	}
	return t.Format(ExpirationLayout)
}

// FormatPaymentID renders a numeric payment id as the string used everywhere else in
// the system (invoice.ExternalID, payment.ExternalID).
func FormatPaymentID(id int64) string { return strconv.FormatInt(id, 10) }

// IdentificationTypeFor picks CPF or CNPJ from the digit count of a Brazilian
// document. Anything else defaults to CPF, which is what Mercado Pago validates
// against and therefore produces the clearest rejection.
func IdentificationTypeFor(document string) string {
	if len(OnlyDigits(document)) == 14 {
		return IdentificationCNPJ
	}
	return IdentificationCPF
}

// OnlyDigits strips formatting from a document number. Mercado Pago rejects a CPF
// containing dots or dashes.
func OnlyDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SplitName splits a full name into the first_name / last_name pair Mercado Pago
// expects. A single-word name yields an empty last name, which the API accepts.
func SplitName(full string) (first, last string) {
	fields := strings.Fields(strings.TrimSpace(full))
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return fields[0], ""
	default:
		return fields[0], strings.Join(fields[1:], " ")
	}
}
