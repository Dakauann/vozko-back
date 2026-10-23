package cmcom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"

	"vozko/domain/carriermessaging"
)

const (
	providerName       = "cmcom"
	headerProductToken = "X-CM-PRODUCTTOKEN"
	contentTypeJSON    = "application/json"
	maxResponseBytes   = 1 << 20
)

var (
	ErrProviderResponse = errors.New("cmcom: unexpected provider response")
	ErrSenderIDs        = errors.New("cmcom: the sender is missing its provider ids")
)

var (
	_ carriermessaging.Provider    = (*Client)(nil)
	_ carriermessaging.SMSProvider = (*Client)(nil)
)

type Client struct {
	productToken string
	accountID    string
	gatewayURL   string
	apiURL       string
	http         *http.Client

	discoveryMu sync.Mutex
	discovered  *channelIDs
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return &Client{
		productToken: cfg.ProductToken,
		accountID:    cfg.AccountID,
		gatewayURL:   cfg.GatewayURL,
		apiURL:       cfg.APIURL,
		http:         client,
	}, nil
}

func (c *Client) Name() string {
	return providerName
}

type response struct {
	status int
	body   []byte
}

func (c *Client) do(ctx context.Context, method, url string, payload any, lost error) (response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return response{}, fmt.Errorf("cmcom: encode request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	var wrote atomic.Bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wrote.Store(true)
			}
		},
	})
	req, err := http.NewRequestWithContext(traced, method, url, body)
	if err != nil {
		return response{}, fmt.Errorf("cmcom: build request: %w", err)
	}
	req.Header.Set(headerProductToken, c.productToken)
	req.Header.Set("Accept", contentTypeJSON)
	if payload != nil {
		req.Header.Set("Content-Type", contentTypeJSON)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if wrote.Load() {
			return response{}, fmt.Errorf("%w: %s %s left but its answer was lost: %v", lost, method, url, err)
		}
		return response{}, fmt.Errorf("%w: %s %s: %v", carriermessaging.ErrProviderUnavailable, method, url, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, fmt.Errorf("%w: reading the answer to %s %s: %v", lost, method, url, err)
	}
	if len(raw) > maxResponseBytes {
		return response{}, fmt.Errorf("%w: %w: the answer to %s %s is too large", ErrProviderResponse, lost, method, url)
	}
	return response{status: res.StatusCode, body: raw}, nil
}

type problemDetails struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func statusError(res response, operation string, serverFailure error) error {
	var problem problemDetails
	_ = json.Unmarshal(res.body, &problem)
	detail := problem.Detail
	if detail == "" {
		detail = problem.Title
	}
	class := carriermessaging.ErrProviderRefused
	switch {
	case res.status == http.StatusTooManyRequests:
		class = carriermessaging.ErrProviderUnavailable
	case res.status >= http.StatusInternalServerError:
		class = serverFailure
	}
	return fmt.Errorf("%w: %s answered %d %s", class, operation, res.status, detail)
}
