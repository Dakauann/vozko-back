package tiktok

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

	"vozko/domain/cache"
	ttdomain "vozko/domain/tiktok"
)

const DefaultBaseURL = "https://business-api.tiktok.com/open_api/v1.3"

const (
	requestTimeout     = 30 * time.Second
	maxAttempts        = 3
	maxJSONBytes       = 8 << 20
	defaultLimiterWait = 2 * time.Second
	maxLimiterChecks   = 5
	maxRetryAfter      = 5 * time.Second
	contentTypeJSON    = "application/json"
	headerAccessToken  = "Access-Token"
)

type Config struct {
	BaseURL            string
	AppID              string
	AppSecret          string
	RedirectURI        string
	AuthorizeURL       string
	HTTPClient         *http.Client
	MediaHTTPClient    *http.Client
	RateLimiterFactory cache.RateLimiterFactory
	AppPerMinute       int
	Backoff            func(attempt int) time.Duration
	MaxLimiterWait     time.Duration
}

type Client struct {
	baseURL        string
	appID          string
	appSecret      string
	redirectURI    string
	authorizeURL   *url.URL
	http           *http.Client
	media          *http.Client
	app            cache.RateLimiter
	perFamily      map[Family]cache.RateLimiter
	background     map[Family]cache.RateLimiter
	backoff        func(attempt int) time.Duration
	maxLimiterWait time.Duration
}

var (
	errLimiterFactoryRequired = errors.New("tiktok: client requires a rate limiter factory")
	errAppCredentialsRequired = errors.New("tiktok: client requires the app id and the app secret")
	errResponseTooLarge       = errors.New("tiktok: response body too large")
)

func NewClient(cfg Config) (*Client, error) {
	if cfg.RateLimiterFactory == nil {
		return nil, errLimiterFactoryRequired
	}
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, errAppCredentialsRequired
	}
	authorize, err := ParseAuthorizeURL(cfg.AuthorizeURL)
	if err != nil {
		return nil, err
	}
	if err := ValidateRedirectURI(cfg.RedirectURI); err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	mediaClient := cfg.MediaHTTPClient
	if mediaClient == nil {
		mediaClient = &http.Client{Timeout: 2 * time.Minute}
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}
	wait := cfg.MaxLimiterWait
	if wait <= 0 {
		wait = defaultLimiterWait
	}
	appPerMinute := cfg.AppPerMinute
	if appPerMinute <= 0 {
		appPerMinute = DefaultAppPerMinute
	}
	c := &Client{
		baseURL:        base,
		appID:          strings.TrimSpace(cfg.AppID),
		appSecret:      strings.TrimSpace(cfg.AppSecret),
		redirectURI:    strings.TrimSpace(cfg.RedirectURI),
		authorizeURL:   authorize,
		http:           httpClient,
		media:          mediaClient,
		app:            cfg.RateLimiterFactory(appLimiterPrefix, appPerMinute, limiterWindow),
		perFamily:      make(map[Family]cache.RateLimiter, len(familyBudgets)),
		background:     make(map[Family]cache.RateLimiter, len(familyBudgets)),
		backoff:        backoff,
		maxLimiterWait: wait,
	}
	for _, f := range Families() {
		c.perFamily[f] = cfg.RateLimiterFactory(f.limiterPrefix(), f.Budget(), limiterWindow)
		c.background[f] = cfg.RateLimiterFactory(f.backgroundPrefix(), f.BackgroundBudget(), limiterWindow)
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
	creds       *ttdomain.Credentials
	body        []byte
	contentType string
	retrySafe   bool
}

type envelope struct {
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, req request) (json.RawMessage, error) {
	if _, ok := c.perFamily[req.family]; !ok {
		return nil, fmt.Errorf("tiktok: unknown endpoint family %q", req.family)
	}
	key := appFamilyKey(req.family)
	if req.creds != nil {
		if !ttdomain.ValidOpaqueID(req.creds.OpenID) || strings.TrimSpace(req.creds.AccessToken) == "" {
			return nil, ttdomain.ErrNeedsReconnect
		}
		key = accountFamilyKey(req.creds.OpenID, req.family)
	}
	retrySafe := req.retrySafe || req.method == http.MethodGet
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleep(ctx, c.retryDelay(attempt, lastErr)); err != nil {
				return nil, err
			}
		}
		if err := c.acquire(ctx, c.app, appLimiterKey); err != nil {
			return nil, err
		}
		if err := c.acquire(ctx, c.perFamily[req.family], key); err != nil {
			return nil, err
		}
		if ttdomain.IsBackground(ctx) {
			if err := c.acquire(ctx, c.background[req.family], key); err != nil {
				return nil, err
			}
		}
		status, header, raw, err := c.send(ctx, req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = &Error{Code: CodeTransport, Ambiguous: !retrySafe, Err: err}
			if errors.Is(err, errResponseTooLarge) {
				lastErr = &Error{Code: CodeResponseTooLarge, Ambiguous: !retrySafe, Err: err}
				return nil, lastErr
			}
			if retrySafe {
				continue
			}
			return nil, lastErr
		}
		data, apiErr := parseEnvelope(status, raw)
		if apiErr == nil {
			return data, nil
		}
		apiErr.retryAfter = retryAfterHeader(header)
		lastErr = apiErr
		if apiErr.Code == CodeRateLimited || status == http.StatusTooManyRequests {
			continue
		}
		if apiErr.Code == CodeSystemError || status >= 500 {
			if retrySafe {
				continue
			}
			apiErr.Ambiguous = true
			return nil, apiErr
		}
		return nil, apiErr
	}
	return nil, lastErr
}

func parseEnvelope(status int, raw []byte) (json.RawMessage, *Error) {
	var env envelope
	decodeErr := json.Unmarshal(raw, &env)
	if status < 200 || status >= 300 {
		e := &Error{HTTPStatus: status, Message: truncate(string(raw), 200)}
		if decodeErr == nil {
			e.Code, e.Message, e.RequestID = env.Code, env.Message, env.RequestID
		}
		return nil, e
	}
	if decodeErr != nil {
		return nil, &Error{HTTPStatus: status, Code: CodeDecode, Message: truncate(string(raw), 200), Err: decodeErr}
	}
	if env.Code != 0 {
		return nil, &Error{HTTPStatus: status, Code: env.Code, Message: env.Message, RequestID: env.RequestID}
	}
	return env.Data, nil
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
			return &Error{Code: CodeLocalRateLimited, Message: fmt.Sprintf("local rate limit reached for %s, retry in %s", key, retryAfter), retryAfter: retryAfter}
		}
		if err := sleep(ctx, retryAfter); err != nil {
			return err
		}
	}
	return &Error{Code: CodeLocalRateLimited, Message: "local rate limit reached for " + key}
}

func (c *Client) send(ctx context.Context, req request) (int, http.Header, []byte, error) {
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
		return 0, nil, nil, err
	}
	httpReq.Header.Set("Accept", contentTypeJSON)
	if req.contentType != "" {
		httpReq.Header.Set("Content-Type", req.contentType)
	}
	if req.creds != nil {
		httpReq.Header.Set(headerAccessToken, req.creds.AccessToken)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := readCapped(resp.Body, maxJSONBytes)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, raw, nil
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: over %d bytes", errResponseTooLarge, limit)
	}
	return raw, nil
}

func (c *Client) getJSON(ctx context.Context, req request, out any) error {
	req.method = http.MethodGet
	data, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	return decodeData(data, out)
}

func (c *Client) postJSON(ctx context.Context, req request, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req.method = http.MethodPost
	req.body = raw
	req.contentType = contentTypeJSON
	data, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := decodeData(data, out); err != nil {
		if e, ok := asError(err); ok {
			e.Ambiguous = !req.retrySafe
		}
		return err
	}
	return nil
}

func decodeData(data json.RawMessage, out any) error {
	if out == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		return &Error{Code: CodeDecode, Message: "response has no data"}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Code: CodeDecode, Message: truncate(string(data), 200), Err: err}
	}
	return nil
}

func jsonList(values []string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
