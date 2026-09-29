package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vozko/domain/decision"
	"vozko/infra/ai/aibilling"
)

const DefaultBaseURL = "https://openrouter.ai/api/alpha"

type Billing interface {
	Publish(workspaceID, model string, promptTokens, completionTokens int, providerCostMicros int64)
}

type Config struct {
	APIKey      string
	BaseURL     string
	Model       string
	HTTPReferer string
	XTitle      string
	Timeout     time.Duration
}

type Client struct {
	cfg         Config
	http        *http.Client
	billing     Billing
	retryDelays []time.Duration
}

func New(cfg Config, billing Billing) (*Client, error) {
	if billing == nil {
		return nil, errors.New("typesafe: billing is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("typesafe: api key and model are required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &Client{
		cfg:         cfg,
		http:        &http.Client{Timeout: cfg.Timeout},
		billing:     billing,
		retryDelays: []time.Duration{200 * time.Millisecond, 600 * time.Millisecond},
	}, nil
}

func (c *Client) Decide(ctx context.Context, request decision.Request) (decision.Result, error) {
	if err := request.Validate(); err != nil {
		return decision.Result{}, err
	}
	body, err := json.Marshal(wireRequest{Model: c.cfg.Model, State: request.State, Questions: toWireQuestions(request.Questions)})
	if err != nil {
		return decision.Result{}, fmt.Errorf("%w: encoding the request: %v", decision.ErrInvalidRequest, err)
	}
	raw, err := c.postWithRetries(ctx, body)
	if err != nil {
		return decision.Result{}, err
	}
	var response wireResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return decision.Result{}, fmt.Errorf("%w: unreadable response: %v", decision.ErrUnavailable, err)
	}
	result := response.toResult(c.cfg.Model)
	c.bill(request.WorkspaceID, result)
	if err := request.Accepts(result); err != nil {
		return decision.Result{}, err
	}
	return result, nil
}

func (c *Client) bill(workspaceID string, result decision.Result) {
	if result.CostMicros <= 0 && result.InputTokens <= 0 {
		return
	}
	go c.billing.Publish(workspaceID, result.Model, result.InputTokens, 0, result.CostMicros)
}

func (c *Client) postWithRetries(ctx context.Context, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= len(c.retryDelays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v (last error: %v)", decision.ErrUnavailable, ctx.Err(), lastErr)
			case <-time.After(c.retryDelays[attempt-1]):
			}
		}
		raw, retry, err := c.post(ctx, body)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, fmt.Errorf("%w: %v", decision.ErrUnavailable, lastErr)
}

func (c *Client) post(ctx context.Context, body []byte) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/decisions", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.HTTPReferer != "" {
		req.Header.Set("HTTP-Referer", c.cfg.HTTPReferer)
	}
	if c.cfg.XTitle != "" {
		req.Header.Set("X-Title", c.cfg.XTitle)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, true, err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return raw, false, nil
	}
	return nil, retryable(res.StatusCode), fmt.Errorf("status %d: %s", res.StatusCode, truncate(string(raw), 300))
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 || status >= 500
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type wireRequest struct {
	Model     string                  `json:"model"`
	State     any                     `json:"state"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria"`
}

func toWireQuestions(questions map[string]decision.Question) map[string]wireQuestion {
	out := make(map[string]wireQuestion, len(questions))
	for id, question := range questions {
		out[id] = wireQuestion{Type: string(question.Kind), Instructions: question.Instructions, Criteria: criteria(question)}
	}
	return out
}

func criteria(question decision.Question) any {
	switch question.Kind {
	case decision.KindChoice:
		options := make(map[string]string, len(question.Options))
		for _, option := range question.Options {
			options[option.Key] = option.Description
		}
		return options
	case decision.KindYesNo:
		return map[string]string{"true": question.Yes, "false": question.No}
	default:
		return question.Levels
	}
}

type wireResponse struct {
	Answers map[string]wireAnswer `json:"answers"`
	Usage   struct {
		InputTokens int     `json:"input_tokens"`
		Cost        float64 `json:"cost"`
	} `json:"usage"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Noul          float64            `json:"noul"`
	Score         float64            `json:"score"`
}

func (r wireResponse) toResult(model string) decision.Result {
	answers := make(map[string]decision.Answer, len(r.Answers))
	for id, answer := range r.Answers {
		answers[id] = decision.Answer{
			Kind:          decision.Kind(answer.Type),
			Choice:        answer.Choice,
			Probabilities: answer.Probabilities,
			Confidence:    answer.Confidence,
			Yes:           answer.Noul,
			Score:         answer.Score,
		}
	}
	return decision.Result{
		Model:       model,
		Answers:     answers,
		InputTokens: r.Usage.InputTokens,
		CostMicros:  aibilling.CostToMicros(r.Usage.Cost),
	}
}
