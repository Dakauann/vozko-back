package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	DefaultGraphVersion = "v26.0"
	GraphHost           = "graph.facebook.com"
	maxPageRequests     = 200
	graphTimeLayout     = "2006-01-02T15:04:05-0700"
)

type Config struct {
	AppSecret    string
	GraphVersion string
	HTTPClient   *http.Client
}

type Gateway struct {
	client     *meta.Client
	now        func() time.Time
	wait       func(ctx context.Context, d time.Duration) error
	mu         sync.Mutex
	pageTokens map[string]pageToken
}

type pageToken struct {
	value   string
	expires time.Time
}

func NewGateway(cfg Config) (*Gateway, error) {
	return newGateway(cfg, 0)
}

func newGateway(cfg Config, maxRetries int) (*Gateway, error) {
	version := strings.TrimSpace(cfg.GraphVersion)
	if version == "" {
		version = DefaultGraphVersion
	}
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: version,
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
		MaxRetries: maxRetries,
	})
	if err != nil {
		return nil, err
	}
	return &Gateway{client: client, now: time.Now, wait: sleep, pageTokens: map[string]pageToken{}}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (g *Gateway) do(ctx context.Context, req meta.Request, out any) error {
	return remoteError(g.client.Do(ctx, req, out))
}

func remoteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if me, ok := meta.AsError(err); ok {
		return &advertising.RemoteError{
			Kind:        failureOf(me),
			Code:        me.Code,
			Subcode:     me.Subcode,
			Message:     me.Message,
			UserTitle:   me.UserTitle,
			UserMessage: me.UserMsg,
		}
	}
	var requestErr *meta.RequestError
	if errors.As(err, &requestErr) {
		return fmt.Errorf("%w: %w", &advertising.RemoteError{Kind: advertising.FailureUnknown, Message: requestErr.Error()}, err)
	}
	return err
}

func failureOf(me *meta.Error) advertising.Failure {
	switch {
	case me.NeedsReauth():
		return advertising.FailureReauth
	case me.IsRateLimit() || me.Retryable():
		return advertising.FailureRetryable
	case me.IsPermission() || me.Code == meta.CodeAccessLevel:
		return advertising.FailurePermission
	case me.Code != 0:
		return advertising.FailureRejected
	}
	return advertising.FailureUnknown
}

type graphPage[T any] struct {
	Data   []T `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

func collect[T any](ctx context.Context, g *Gateway, path, token string, query url.Values) ([]T, error) {
	var items []T
	after := ""
	for range maxPageRequests {
		q := url.Values{}
		for k, vs := range query {
			q[k] = append([]string(nil), vs...)
		}
		if after != "" {
			q.Set("after", after)
		}
		var page graphPage[T]
		if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Data...)
		if page.Paging.Next == "" {
			return items, nil
		}
		if page.Paging.Cursors.After == "" {
			return nil, fmt.Errorf("marketing: %s has a next page without a cursor", path)
		}
		after = page.Paging.Cursors.After
	}
	return nil, fmt.Errorf("marketing: %s did not finish paging after %d requests", path, maxPageRequests)
}

type graphNumber string

func (n *graphNumber) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*n = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*n = graphNumber(strings.TrimSpace(s))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*n = graphNumber(number.String())
	return nil
}

func (n graphNumber) minorUnits(field string) (int64, error) {
	if n == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("marketing: %s %q is not an integer amount", field, string(n))
	}
	return v, nil
}

func (n graphNumber) wholePart(field string) (int64, error) {
	if n == "" {
		return 0, nil
	}
	whole, fraction, _ := strings.Cut(string(n), ".")
	v, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || v < 0 || strings.Trim(fraction, "0123456789") != "" {
		return 0, fmt.Errorf("marketing: %s %q is not a count", field, string(n))
	}
	return v, nil
}

func graphTime(field, raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(graphTimeLayout, raw)
	if err != nil {
		return nil, fmt.Errorf("marketing: %s %q is not a graph time", field, raw)
	}
	utc := t.UTC()
	return &utc, nil
}

func jsonValue(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (g *Gateway) acknowledged(ctx context.Context, req meta.Request) error {
	var out struct {
		Success *bool `json:"success"`
	}
	if err := g.do(ctx, req, &out); err != nil {
		return err
	}
	if out.Success == nil || !*out.Success {
		return fmt.Errorf("marketing: %s %s was not acknowledged", req.Method, req.Path)
	}
	return nil
}

func (g *Gateway) created(ctx context.Context, req meta.Request, what string) (string, error) {
	var out struct {
		ID meta.GraphID `json:"id"`
	}
	if err := g.do(ctx, req, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("marketing: %s created without an id", what)
	}
	return out.ID.String(), nil
}

func accountPath(metaAccountID string) (string, error) {
	id := advertising.NormalizeAccountID(metaAccountID)
	if id == "" {
		return "", fmt.Errorf("marketing: ad account id is required")
	}
	return "/act_" + id, nil
}

func objectPath(metaID string) (string, error) {
	id := strings.TrimSpace(metaID)
	if id == "" || strings.Contains(id, "/") {
		return "", fmt.Errorf("marketing: invalid object id %q", metaID)
	}
	return "/" + id, nil
}

func (g *Gateway) get(ctx context.Context, path, token, fields string, out any) error {
	q := url.Values{}
	q.Set("fields", fields)
	return g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, out)
}

type graphList[T any] []T

func (l *graphList[T]) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*l = nil
		return nil
	}
	if trimmed[0] == '[' {
		var items []T
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		*l = items
		return nil
	}
	var page struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return err
	}
	*l = page.Data
	return nil
}

type graphRef struct {
	ID   meta.GraphID `json:"id"`
	Name string       `json:"name,omitempty"`
}

func (r *graphRef) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed != "" && trimmed[0] != '{' {
		r.Name = ""
		return json.Unmarshal(data, &r.ID)
	}
	type plain graphRef
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = graphRef(out)
	return nil
}

func refsOf(refs []advertising.TargetRef) []graphRef {
	out := make([]graphRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, graphRef{ID: meta.GraphID(r.ID), Name: r.Name})
	}
	return out
}

func targetRefs(refs []graphRef) []advertising.TargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]advertising.TargetRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, advertising.TargetRef{ID: r.ID.String(), Name: r.Name})
	}
	return out
}

func graphIDs(refs []graphRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.ID.String())
	}
	return out
}

func timeParam(t time.Time) string { return t.UTC().Format(time.RFC3339) }
