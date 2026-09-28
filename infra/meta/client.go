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
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultGraphVersion = "v25.0"

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	maxResponseBytes  = 16 << 20
	maxMediaBytes     = 100 << 20
)

type Config struct {
	Host       string
	APIVersion string
	AppSecret  string
	MaxRetries int
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Usage struct {
	CallCount              int
	TotalCPUTime           int
	TotalTime              int
	EstimatedRegainMinutes int
}

func (u Usage) Percent() int {
	return max(u.CallCount, u.TotalCPUTime, u.TotalTime)
}

type Client struct {
	cfg     Config
	http    *http.Client
	baseURL string
	sleep   func(context.Context, time.Duration) error

	mu          sync.RWMutex
	lastUsage   Usage
	objectUsage map[string]Usage
}

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
		cfg:         cfg,
		http:        httpClient,
		baseURL:     "https://" + host + "/" + version,
		sleep:       sleepContext,
		objectUsage: make(map[string]Usage),
	}, nil
}

func VersionOr(v string) string {
	if strings.TrimSpace(v) == "" {
		return DefaultGraphVersion
	}
	return strings.TrimSpace(v)
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) LastUsage() Usage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUsage
}

func (c *Client) UsageFor(objectID string) (Usage, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	u, ok := c.objectUsage[objectID]
	return u, ok
}

type Request struct {
	Method     string
	Path       string
	Token      string
	Query      url.Values
	Body       any
	Form       url.Values
	File       *FilePart
	Idempotent bool
}

type FilePart struct {
	Field       string
	FileName    string
	ContentType string
	Data        []byte
}

func (r Request) retrySafe() bool {
	return r.Method == http.MethodGet || r.Method == http.MethodDelete || r.Idempotent
}

func (c *Client) Do(ctx context.Context, req Request, out any) error {
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, backoff(attempt)); err != nil {
				return err
			}
		}

		err := c.attempt(ctx, req, out)
		if err == nil {
			return nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !shouldRetry(req, err) {
			return err
		}
		if me, ok := AsError(err); ok {
			log.Printf("[meta] retrying %s %s attempt=%d code=%d subcode=%d trace=%s",
				req.Method, req.Path, attempt+1, me.Code, me.Subcode, me.FBTraceID)
		}
	}
	return lastErr
}

func shouldRetry(req Request, err error) bool {
	if me, ok := AsError(err); ok {
		if req.retrySafe() {
			return me.Retryable()
		}
		return me.IsTransient || me.IsRateLimit()
	}
	return req.retrySafe() && IsRetryable(err)
}

func (c *Client) attempt(ctx context.Context, req Request, out any) error {
	u := c.baseURL + req.Path

	query := url.Values{}
	for k, vs := range req.Query {
		for _, v := range vs {
			query.Add(k, v)
		}
	}
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
	case req.File != nil:
		body, ct, err := multipartBody(req.Form, req.File)
		if err != nil {
			return &RequestError{Op: "build multipart", Err: err}
		}
		bodyReader, contentType = body, ct
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

	return c.execute(httpReq, req.Method+" "+req.Path, out)
}

func multipartBody(fields url.Values, file *FilePart) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, values := range fields {
		for _, v := range values {
			if err := writer.WriteField(key, v); err != nil {
				return nil, "", err
			}
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, file.Field, file.FileName))
	header.Set("Content-Type", file.ContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(file.Data); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func (c *Client) DoRaw(ctx context.Context, httpReq *http.Request, out any) error {

	return c.execute(httpReq.WithContext(ctx), httpReq.Method+" "+httpReq.URL.Path, out)
}

func (c *Client) execute(httpReq *http.Request, label string, out any) error {
	started := time.Now()

	resp, err := c.http.Do(httpReq)
	if err != nil {
		log.Printf("[meta] %s transport error after %s: %v", label, time.Since(started).Round(time.Millisecond), err)
		return &RequestError{Op: label, Err: err}
	}
	defer resp.Body.Close()

	c.recordUsage(resp.Header)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &RequestError{Op: "read body", Err: err}
	}

	log.Printf("[meta] %s -> %d in %s (%d bytes)", label, resp.StatusCode,
		time.Since(started).Round(time.Millisecond), len(raw))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[meta] %s failed, body: %s", label, truncate(raw, 1024))
		return decodeError(resp.StatusCode, raw)
	}

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

func (c *Client) FetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", &RequestError{Op: "new media request", Err: err}
	}
	httpReq.Header.Set("Accept", "image/avif,image/webp,image/jpeg,image/*,video/*,*/*;q=0.8")
	httpReq.Header.Set("User-Agent", "Vozko/MetaMediaProxy")
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, "", &RequestError{Op: "read media", Err: err}
	}
	if len(data) > maxMediaBytes {
		return nil, "", &RequestError{Op: "read media", Err: fmt.Errorf("media response exceeds %d bytes", maxMediaBytes)}
	}
	return data, resp.Header.Get("Content-Type"), nil
}

type usageEntry struct {
	CallCount             int `json:"call_count"`
	TotalCPUTime          int `json:"total_cputime"`
	TotalTime             int `json:"total_time"`
	EstimatedTimeToRegain int `json:"estimated_time_to_regain_access"`
}

func (c *Client) recordUsage(h http.Header) {
	if raw := h.Get("X-App-Usage"); raw != "" {
		var u usageEntry
		if err := json.Unmarshal([]byte(raw), &u); err == nil {
			c.mu.Lock()
			c.lastUsage = Usage{CallCount: u.CallCount, TotalCPUTime: u.TotalCPUTime, TotalTime: u.TotalTime}
			c.mu.Unlock()
		}
	}
	if raw := h.Get("X-Business-Use-Case-Usage"); raw != "" {
		var byObject map[string][]usageEntry
		if err := json.Unmarshal([]byte(raw), &byObject); err != nil {
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for objectID, entries := range byObject {
			var merged Usage
			for _, e := range entries {
				merged.CallCount = max(merged.CallCount, e.CallCount)
				merged.TotalCPUTime = max(merged.TotalCPUTime, e.TotalCPUTime)
				merged.TotalTime = max(merged.TotalTime, e.TotalTime)
				merged.EstimatedRegainMinutes = max(merged.EstimatedRegainMinutes, e.EstimatedTimeToRegain)
			}
			c.objectUsage[objectID] = merged
		}
	}
}

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

func backoff(attempt int) time.Duration {
	base := time.Duration(math.Pow(2, float64(attempt))) * 250 * time.Millisecond
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	//nolint:gosec
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	return base/2 + jitter
}

func sleepContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
