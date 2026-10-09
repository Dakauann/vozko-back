package opencage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/cep"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

const (
	ProviderName       = "opencage"
	defaultBaseURL     = "https://api.opencagedata.com/geocode/v1/json"
	defaultTimeout     = 10 * time.Second
	maxResponseBytes   = 1 << 20
	maxLimiterWait     = 2 * time.Second
	maxLimiterAttempts = 3
	limiterKey         = "provider"
	resultLimit        = "2"
)

var ErrNotConfigured = errors.New("opencage: an API key and a rate limiter are required")

type Config struct {
	APIKey     string
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
	Limiter    cache.RateLimiter
}

type Client struct {
	cfg     Config
	http    *http.Client
	limiter cache.RateLimiter
	sleep   func(context.Context, time.Duration) error
	now     func() time.Time
}

var _ geo.Geocoder = (*Client)(nil)

func NewClient(cfg Config) (*Client, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.APIKey == "" || cfg.Limiter == nil {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: httpClient, limiter: cfg.Limiter, sleep: sleepContext, now: func() time.Time { return time.Now().UTC() }}, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Error struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("opencage: status %d: %s", e.Status, e.Message)
}

func (e *Error) reason() geo.UnavailableReason {
	return geocoding.ReasonOfStatus(e.Status)
}

var errInvalidAnswer = errors.New("opencage: unreadable answer")

func (c *Client) Geocode(ctx context.Context, q geo.Query) (geo.Outcome, error) {
	text := queryText(q)
	if text == "" {
		return geo.NotFound(), nil
	}
	if refusal, ok := c.takeSlot(ctx); !ok {
		return refusal, nil
	}
	answer, err := c.call(ctx, text)
	if err != nil {
		return unavailableFor(err), nil
	}
	return c.outcomeOf(answer), nil
}

func unavailableFor(err error) geo.Outcome {
	var apiErr *Error
	switch {
	case errors.As(err, &apiErr):
		return geo.Unavailable(apiErr.reason(), apiErr.RetryAfter)
	case errors.Is(err, errInvalidAnswer):
		return geo.Unavailable(geo.ReasonInvalidAnswer, 0)
	}
	return geo.Unavailable(geo.ReasonProviderDown, 0)
}

func (c *Client) takeSlot(ctx context.Context) (geo.Outcome, bool) {
	for i := 0; i < maxLimiterAttempts; i++ {
		allowed, retryAfter, err := c.limiter.Allow(limiterKey)
		if err != nil {
			return geo.Unavailable(geo.ReasonRateLimiterDown, 0), false
		}
		if allowed {
			return geo.Outcome{}, true
		}
		if retryAfter > maxLimiterWait {
			return geo.Unavailable(geo.ReasonRateLimited, retryAfter), false
		}
		if err := c.sleep(ctx, max(retryAfter, 100*time.Millisecond)); err != nil {
			return geo.Unavailable(geo.ReasonRateLimited, 0), false
		}
	}
	return geo.Unavailable(geo.ReasonRateLimited, maxLimiterWait), false
}

func queryText(q geo.Query) string {
	p := q.Postal.Normalize()
	var parts []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	add(p.Street)
	add(p.Number)
	add(p.District)
	switch {
	case p.City != "" && p.State != "":
		add(p.City + " - " + p.State)
	default:
		add(p.City)
		add(p.State)
	}
	if zip, err := cep.Parse(p.ZipCode); err == nil {
		add(cep.Format(zip))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(append(parts, "Brasil"), ", ")
}

type apiResult struct {
	Components struct {
		Type        string `json:"_type"`
		CountryCode string `json:"country_code"`
	} `json:"components"`
	Geometry struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"geometry"`
}

type apiAnswer struct {
	Results []apiResult `json:"results"`
	Status  struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Rate struct {
		Reset int64 `json:"reset"`
	} `json:"rate"`
}

func (c *Client) call(ctx context.Context, text string) (apiAnswer, error) {
	params := url.Values{}
	params.Set("q", text)
	params.Set("key", c.cfg.APIKey)
	params.Set("countrycode", "br")
	params.Set("language", "pt")
	params.Set("limit", resultLimit)
	params.Set("no_annotations", "1")
	params.Set("no_record", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return apiAnswer{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return apiAnswer{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return apiAnswer{}, err
	}
	var answer apiAnswer
	decodeErr := json.Unmarshal(body, &answer)
	if resp.StatusCode != http.StatusOK {
		return apiAnswer{}, &Error{Status: resp.StatusCode, Message: answer.Status.Message, RetryAfter: c.untilReset(answer.Rate.Reset)}
	}
	if decodeErr != nil {
		return apiAnswer{}, fmt.Errorf("%w: %v", errInvalidAnswer, decodeErr)
	}
	return answer, nil
}

func (c *Client) untilReset(reset int64) time.Duration {
	if reset <= 0 {
		return 0
	}
	return max(time.Unix(reset, 0).Sub(c.now()), 0)
}

func precisionOf(kind string) (geo.Precision, bool) {
	switch kind {
	case "building", "house":
		return geo.PrecisionAddress, true
	case "road":
		return geo.PrecisionStreet, true
	case "postcode":
		return geo.PrecisionPostalCode, true
	case "neighbourhood", "suburb", "city_district", "quarter":
		return geo.PrecisionDistrict, true
	case "city", "town", "village", "hamlet", "municipality":
		return geo.PrecisionCity, true
	}
	return "", false
}

func (c *Client) fixOf(r apiResult) (geo.Fix, bool) {
	if !strings.EqualFold(r.Components.CountryCode, "br") {
		return geo.Fix{}, false
	}
	precision, ok := precisionOf(r.Components.Type)
	point := geo.Point{Lat: r.Geometry.Lat, Lng: r.Geometry.Lng}
	if !ok || !point.InBrazil() {
		return geo.Fix{}, false
	}
	return geo.Fix{Point: point, Precision: precision, Source: geo.SourceProvider, Provider: ProviderName, FixedAt: c.now()}, true
}

func (c *Client) outcomeOf(answer apiAnswer) geo.Outcome {
	var fixes []geo.Fix
	for _, r := range answer.Results {
		if fix, ok := c.fixOf(r); ok {
			fixes = append(fixes, fix)
		}
	}
	return geo.OutcomeOfAnswers(fixes)
}
