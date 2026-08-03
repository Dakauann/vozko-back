package meta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	maxResponseBytes  = 16 << 20 // 16 MiB, media proxying reads larger bodies separately
)

// Config configures a Graph client.
type Config struct {
	// Host is the API host WITHOUT scheme, e.g. "graph.instagram.com".
	Host string
	// APIVersion is pinned explicitly, e.g. "v25.0". Never call unversioned:
	// each version has a published sunset date.
	APIVersion string
	// AppSecret enables appsecret_proof, which Meta recommends for server-side
	// calls so a leaked token cannot be replayed without the secret.
	AppSecret string
	// MaxRetries bounds transient-failure retries. Zero uses the default.
	MaxRetries int
	// Timeout per attempt. Zero uses the default.
	Timeout time.Duration
	// HTTPClient allows tests to inject a transport.
	HTTPClient *http.Client
}

// Usage is Meta's rate-limit telemetry, parsed from the response headers. Meta
// does not document these headers for Instagram messaging, so all fields are
// best-effort and may be absent.
type Usage struct {
	CallCount      int
	TotalCPUTime   int
	TotalTime      int
	EstimatedBlock int
}

// Client is a Graph API client.
type Client struct {
	cfg     Config
	http    *http.Client
	baseURL string

	mu        sync.RWMutex
	lastUsage Usage
}

// NewClient builds a Graph client. Host and APIVersion are required.
func NewClient(cfg Config) (*Client, error) {
	host := strings.TrimSpace(strings.TrimSuffix(cfg.Host, "/"))
	if host == "" {
		return nil, fmt.Errorf("meta: host is required")
	}
	version := strings.TrimSpace(cfg.APIVersion)
	if version == "" {
		return nil, fmt.Errorf("meta: api version is required (pin it explicitly)")
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{
		cfg:     cfg,
		http:    httpClient,
		baseURL: "https://" + host + "/" + version,
	}, nil
}

// BaseURL returns the versioned base URL, for building edge paths.
func (c *Client) BaseURL() string { return c.baseURL }

// LastUsage returns the most recently observed rate-limit telemetry.
func (c *Client) LastUsage() Usage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUsage
}

// Request describes one Graph call.
type Request struct {
	Method string
	// Path is the edge path relative to the versioned base, e.g. "/me/messages"
	// or "/17841.../media".
	Path string
	// Token is the access token. Sent as a bearer header rather than a query
	// param so it does not leak into logs or proxy access records.
	Token string
	// Query holds additional query parameters.
	Query url.Values
	// Body is marshalled as JSON when non-nil.
	Body any
	// Form, when set, is sent as application/x-www-form-urlencoded instead of
	// JSON. Some Graph edges only accept form encoding.
	Form url.Values
}

// Do performs a Graph call and decodes a successful JSON body into out.
//
// Transient failures are retried with exponential backoff plus jitter, bounded
// by MaxRetries and by the context deadline. Non-retryable errors return
// immediately as a *Error so callers can branch on code/subcode.
func (c *Client) Do(ctx context.Context, req Request, out any) error {
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := c.attempt(ctx, req, out)
		if err == nil {
			return nil
		}
		lastErr = err

		// Never retry a context failure or a non-retryable API error.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !IsRetryable(err) {
			return err
		}
		if me, ok := AsError(err); ok {
			log.Printf("[meta] retryable error attempt=%d code=%d subcode=%d trace=%s",
				attempt+1, me.Code, me.Subcode, me.FBTraceID)
		}
	}
	return lastErr
}

func (c *Client) attempt(ctx context.Context, req Request, out any) error {
	u := c.baseURL + req.Path

	query := url.Values{}
	for k, vs := range req.Query {
		for _, v := range vs {
			query.Add(k, v)
		}
	}
	// appsecret_proof binds the call to our app secret so a stolen token alone
	// is not enough to use the API.
	if c.cfg.AppSecret != "" && req.Token != "" {
		query.Set("appsecret_proof", AppSecretProof(req.Token, c.cfg.AppSecret))
	}
	if encoded := query.Encode(); encoded != "" {
		u += "?" + encoded
	}

	var (
		bodyReader  io.Reader
		contentType string
	)
	switch {
	case req.Form != nil:
		bodyReader = strings.NewReader(req.Form.Encode())
		contentType = "application/x-www-form-urlencoded"
	case req.Body != nil:
		raw, err := json.Marshal(req.Body)
		if err != nil {
			return &RequestError{Op: "marshal body", Err: err}
		}
		bodyReader = bytes.NewReader(raw)
		contentType = "application/json"
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u, bodyReader)
	if err != nil {
		return &RequestError{Op: "new request", Err: err}
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}

	started := time.Now()

	resp, err := c.http.Do(httpReq)
	if err != nil {
		log.Printf("[meta] %s %s transport error after %s: %v",
			req.Method, req.Path, time.Since(started).Round(time.Millisecond), err)
		return &RequestError{Op: req.Method + " " + req.Path, Err: err}
	}
	defer resp.Body.Close()

	c.recordUsage(resp.Header)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &RequestError{Op: "read body", Err: err}
	}

	log.Printf("[meta] %s %s -> %d in %s (%d bytes)",
		req.Method, req.Path, resp.StatusCode,
		time.Since(started).Round(time.Millisecond), len(raw))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Meta's error body carries code/subcode/fbtrace_id, which is the only way
		// to tell a closed messaging window from a dead token from a rate limit.
		log.Printf("[meta] %s %s failed, body: %s", req.Method, req.Path, truncate(raw, 1024))
		return decodeError(resp.StatusCode, raw)
	}

	// Meta sometimes returns an error envelope with a 200 status.
	if looksLikeError(raw) {
		if apiErr := decodeError(resp.StatusCode, raw); apiErr != nil {
			return apiErr
		}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &RequestError{Op: "decode response", Err: fmt.Errorf("%w (body: %s)", err, truncate(raw, 512))}
	}
	return nil
}

// FetchBytes retrieves a raw asset (a CDN media URL). It deliberately bypasses
// the Graph base URL and token: media URLs are already signed, and they expire,
// which is why they are proxied on demand instead of stored.
func (c *Client) FetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", &RequestError{Op: "new media request", Err: err}
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, "", &RequestError{Op: "fetch media", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &Error{
			HTTPStatus: resp.StatusCode,
			Message:    "media fetch failed with status " + strconv.Itoa(resp.StatusCode),
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, "", &RequestError{Op: "read media", Err: err}
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) recordUsage(h http.Header) {
	raw := h.Get("X-App-Usage")
	if raw == "" {
		raw = h.Get("X-Business-Use-Case-Usage")
	}
	if raw == "" {
		return
	}
	var u struct {
		CallCount    int `json:"call_count"`
		TotalCPUTime int `json:"total_cputime"`
		TotalTime    int `json:"total_time"`
	}
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		return
	}
	c.mu.Lock()
	c.lastUsage = Usage{
		CallCount:    u.CallCount,
		TotalCPUTime: u.TotalCPUTime,
		TotalTime:    u.TotalTime,
	}
	c.mu.Unlock()
}

// AppSecretProof computes the HMAC-SHA256 of the access token keyed with the app
// secret, hex encoded.
func AppSecretProof(token, appSecret string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func decodeError(status int, raw []byte) error {
	var body errorBody
	if err := json.Unmarshal(raw, &body); err != nil || (body.Error.Code == 0 && body.Error.Message == "") {
		return &Error{
			HTTPStatus: status,
			Message:    truncate(raw, 512),
		}
	}
	apiErr := body.Error
	apiErr.HTTPStatus = status
	return &apiErr
}

func looksLikeError(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return bytes.HasPrefix(trimmed, []byte(`{"error"`)) ||
		bytes.Contains(trimmed[:min(len(trimmed), 64)], []byte(`"error"`))
}

// backoff returns an exponentially increasing delay with full jitter, so a fleet
// of consumers hitting the same rate limit does not retry in lockstep.
func backoff(attempt int) time.Duration {
	base := time.Duration(math.Pow(2, float64(attempt))) * 250 * time.Millisecond
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	//nolint:gosec // jitter does not need a cryptographic source
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	return base/2 + jitter
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
