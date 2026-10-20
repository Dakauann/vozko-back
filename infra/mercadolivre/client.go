package mercadolivre

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"vozko/domain/cache"
	mldomain "vozko/domain/mercadolivre"
)

const DefaultBaseURL = "https://api.mercadolibre.com"

const (
	requestTimeout     = 30 * time.Second
	maxAttempts        = 3
	maxJSONBytes       = 8 << 20
	defaultLimiterWait = 2 * time.Second
	maxLimiterChecks   = 5
	maxRetryAfter      = 5 * time.Second
	contentTypeJSON    = "application/json"
	contentTypeForm    = "application/x-www-form-urlencoded"
)

type Config struct {
	BaseURL            string
	HTTPClient         *http.Client
	RateLimiterFactory cache.RateLimiterFactory
	Backoff            func(attempt int) time.Duration
	MaxLimiterWait     time.Duration
}

type Client struct {
	baseURL        string
	http           *http.Client
	perAccount     cache.RateLimiter
	perFamily      map[Family]cache.RateLimiter
	background     cache.RateLimiter
	backoff        func(attempt int) time.Duration
	maxLimiterWait time.Duration
}

var errLimiterFactoryRequired = errors.New("mercadolivre: client requires a rate limiter factory")

var errResponseTooLarge = errors.New("mercadolivre: response body too large")

func NewClient(cfg Config) (*Client, error) {
	if cfg.RateLimiterFactory == nil {
		return nil, errLimiterFactoryRequired
	}
	base, err := endpointURL(cfg.BaseURL, DefaultBaseURL)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre: api base url: %w", err)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}
	wait := cfg.MaxLimiterWait
	if wait <= 0 {
		wait = defaultLimiterWait
	}
	c := &Client{
		baseURL:        base,
		http:           httpClient,
		perAccount:     cfg.RateLimiterFactory(accountLimiterPrefix, AccountPerMinute, limiterWindow),
		perFamily:      make(map[Family]cache.RateLimiter, len(familyBudgets)),
		background:     cfg.RateLimiterFactory(backgroundLimiterPrefix, BackgroundPerMinute, limiterWindow),
		backoff:        backoff,
		maxLimiterWait: wait,
	}
	for _, f := range Families() {
		c.perFamily[f] = cfg.RateLimiterFactory(f.limiterPrefix(), familyBudgets[f], limiterWindow)
	}
	return c, nil
}

func defaultBackoff(attempt int) time.Duration {
	base := 250 * time.Millisecond << max(attempt-1, 0)
	return min(base, 2*time.Second) + time.Duration(rand.Int64N(int64(100*time.Millisecond)))
}

type request struct {
	method      string
	path        string
	query       url.Values
	family      Family
	creds       mldomain.Credentials
	limiterKey  string
	anonymous   bool
	body        []byte
	contentType string
	headers     map[string]string
	maxBytes    int64
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (c *Client) do(ctx context.Context, req request) (*response, error) {
	key := req.limiterKey
	if !req.anonymous {
		if err := req.creds.Validate(); err != nil {
			return nil, err
		}
		key = accountKey(req.creds.UserID)
	}
	if key == "" {
		return nil, mldomain.ErrCredentialsInvalid
	}
	familyLimiter, ok := c.perFamily[req.family]
	if !ok {
		return nil, fmt.Errorf("mercadolivre: unknown endpoint family %q", req.family)
	}
	idempotent := req.method == http.MethodGet
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleep(ctx, c.retryDelay(attempt, lastErr)); err != nil {
				return nil, err
			}
		}
		if err := c.acquire(ctx, c.perAccount, key); err != nil {
			return nil, err
		}
		if mldomain.IsBackground(ctx) {
			if err := c.acquire(ctx, c.background, BackgroundLimiterKey); err != nil {
				return nil, err
			}
		}
		if err := c.acquire(ctx, familyLimiter, req.family.Key()); err != nil {
			return nil, err
		}
		resp, err := c.send(ctx, req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, errResponseTooLarge) {
				return nil, &Error{Code: CodeResponseTooLarge, Ambiguous: !idempotent, Err: err}
			}
			lastErr = &Error{Code: CodeTransport, Ambiguous: !idempotent, Err: err}
			if idempotent {
				continue
			}
			return nil, lastErr
		}
		if resp.status >= 200 && resp.status < 300 {
			return resp, nil
		}
		lastErr = parseError(resp.status, resp.body)
		if mlErr, ok := asError(lastErr); ok {
			mlErr.retryAfter = retryAfterHeader(resp.header)
		}
		if resp.status == http.StatusTooManyRequests {
			continue
		}
		if idempotent && resp.status >= 500 {
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func (c *Client) retryDelay(attempt int, lastErr error) time.Duration {
	delay := c.backoff(attempt - 1)
	if e, ok := asError(lastErr); ok && e.retryAfter > delay {
		delay = min(e.retryAfter, maxRetryAfter)
	}
	return delay
}

func retryAfterHeader(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func (c *Client) acquire(ctx context.Context, limiter cache.RateLimiter, key string) error {
	if limiter == nil {
		return &Error{Code: CodeLimiterUnavailable, Message: "rate limiter missing for " + key}
	}
	for i := 0; i < maxLimiterChecks; i++ {
		allowed, retryAfter, err := limiter.Allow(key)
		if err != nil {
			return &Error{Code: CodeLimiterUnavailable, Message: "rate limiter unavailable for " + key, Err: err}
		}
		if allowed {
			return nil
		}
		if retryAfter <= 0 {
			retryAfter = 100 * time.Millisecond
		}
		if retryAfter > c.maxLimiterWait {
			return &Error{Code: CodeLocalRateLimited, Message: fmt.Sprintf("local rate limit reached for %s, retry in %s", key, retryAfter)}
		}
		if err := sleep(ctx, retryAfter); err != nil {
			return err
		}
	}
	return &Error{Code: CodeLocalRateLimited, Message: "local rate limit reached for " + key}
}

func (c *Client) send(ctx context.Context, req request) (*response, error) {
	target := c.baseURL + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", contentTypeJSON)
	if req.contentType != "" {
		httpReq.Header.Set("Content-Type", req.contentType)
	}
	if !req.anonymous {
		httpReq.Header.Set("Authorization", "Bearer "+req.creds.AccessToken)
	}
	for k, v := range req.headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limit := req.maxBytes
	if limit <= 0 {
		limit = maxJSONBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: over %d bytes", errResponseTooLarge, limit)
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: raw}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) getJSON(ctx context.Context, req request, out any) error {
	req.method = http.MethodGet
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	return decodeJSON(resp, out)
}

func (c *Client) sendJSON(ctx context.Context, req request, payload any, out any) (*response, error) {
	if payload != nil {
		raw, err := encodeJSON(payload)
		if err != nil {
			return nil, err
		}
		req.body = raw
		req.contentType = contentTypeJSON
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	if out != nil {
		if err := decodeJSON(resp, out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func markAcceptedButUnreadable(resp *response, err error) error {
	if resp == nil {
		return err
	}
	if e, ok := asError(err); ok {
		e.Ambiguous = true
	}
	return err
}

func decodeJSON(resp *response, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return &Error{HTTPStatus: resp.status, Code: CodeDecode, Message: truncate(string(resp.body), 200), Err: err}
	}
	return nil
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	raw := bytes.TrimRight(buf.Bytes(), "\n")
	out := make([]byte, 0, len(raw))
	for _, r := range string(raw) {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			out = fmt.Appendf(out, `\u%04x\u%04x`, hi, lo)
		default:
			out = fmt.Appendf(out, `\u%04x`, r)
		}
	}
	return out, nil
}

func pathEscape(segment string) string { return url.PathEscape(segment) }
