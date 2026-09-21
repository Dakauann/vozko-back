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

const DefaultBaseURL = "https://api.mercadopago.com"

const defaultTimeout = 30 * time.Second

const ExpirationLayout = "2006-01-02T15:04:05.000-07:00"

const (
	MinPixExpiry = 30 * time.Minute
	MaxPixExpiry = 30 * 24 * time.Hour
)

var paymentIDPattern = regexp.MustCompile(`^[0-9]+$`)

type Client interface {
	CreatePayment(ctx context.Context, req CreatePaymentRequest, idempotencyKey string) (*Payment, error)
	GetPayment(ctx context.Context, paymentID string) (*Payment, error)
	RefundPayment(ctx context.Context, paymentID string, amount float64, idempotencyKey string) (*Refund, error)
	CancelPayment(ctx context.Context, paymentID string) (*Payment, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type client struct {
	accessToken     string
	baseURL         string
	notificationURL string
	http            HTTPClient
}

type Option func(*client)

func WithHTTPClient(h HTTPClient) Option {
	return func(c *client) {
		if h != nil {
			c.http = h
		}
	}
}

func WithNotificationURL(u string) Option {
	return func(c *client) { c.notificationURL = strings.TrimSpace(u) }
}

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
	ErrNotFound           = errors.New("mercadopago: resource not found")
	ErrUnauthorized       = errors.New("mercadopago: unauthorized")
	ErrInvalidPaymentID   = errors.New("mercadopago: invalid payment id")
	ErrMissingAccessToken = errors.New("mercadopago: access token is not configured")
)

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

func (e *ResponseError) Hint() string {
	haystack := strings.ToLower(e.Message + " " + e.Body + " " + formatCauses(e.Causes))
	for _, h := range knownFailureHints {
		if strings.Contains(haystack, h.match) {
			return h.hint
		}
	}
	return ""
}

func (e *ResponseError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	}
	return nil
}

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

var idempotencyNamespace = uuid.MustParse("6f9c1f4e-3a2b-5d7e-9c11-0b6d2f8a4c73")

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

func FormatPaymentID(id int64) string { return strconv.FormatInt(id, 10) }

func IdentificationTypeFor(document string) string {
	if len(OnlyDigits(document)) == 14 {
		return IdentificationCNPJ
	}
	return IdentificationCPF
}

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
