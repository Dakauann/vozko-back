package postmaster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

const (
	defaultTokenURL   = "https://oauth2.googleapis.com/token"
	defaultAPIBaseURL = "https://gmailpostmastertools.googleapis.com"
	spamRateMetric    = "spam_rate"
	httpTimeout       = 20 * time.Second
	tokenRefreshSlack = time.Minute
	maxResponseBytes  = 1 << 20
)

var (
	ErrConfig       = errors.New("postmaster: incomplete configuration")
	ErrTokenRequest = errors.New("postmaster: token request failed")
	ErrStatsRequest = errors.New("postmaster: stats request failed")
)

type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	TokenURL     string
	APIBaseURL   string
	Client       *http.Client
}

type Monitor struct {
	clientID     string
	clientSecret string
	refreshToken string
	tokenURL     string
	base         string
	client       *http.Client
	now          func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

var _ emailsending.DeliverabilityMonitor = (*Monitor)(nil)

func New(cfg Config) (*Monitor, error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.RefreshToken) == "" {
		return nil, ErrConfig
	}
	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if base == "" {
		base = defaultAPIBaseURL
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Monitor{
		clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, refreshToken: cfg.RefreshToken,
		tokenURL: tokenURL, base: base, client: client, now: time.Now,
	}, nil
}

func (m *Monitor) Name() string { return "google_postmaster" }

type googleDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type queryRequest struct {
	MetricDefinitions []metricDefinition `json:"metricDefinitions"`
	TimeQuery         struct {
		DateList struct {
			Dates []googleDate `json:"dates"`
		} `json:"dateList"`
	} `json:"timeQuery"`
	AggregationGranularity string `json:"aggregationGranularity"`
}

type metricDefinition struct {
	Name       string `json:"name"`
	BaseMetric struct {
		StandardMetric string `json:"standardMetric"`
	} `json:"baseMetric"`
}

type queryResponse struct {
	DomainStats []struct {
		Metric string `json:"metric"`
		Value  struct {
			DoubleValue *float64 `json:"doubleValue"`
			FloatValue  *float64 `json:"floatValue"`
		} `json:"value"`
	} `json:"domainStats"`
}

func (m *Monitor) SpamRate(ctx context.Context, domain string, day time.Time) (emailsending.DomainStats, error) {
	normalized, err := emailaddress.NormalizeDomain(domain)
	if err != nil {
		return emailsending.DomainStats{}, err
	}
	stats := emailsending.DomainStats{Domain: normalized, Day: day.Format("2006-01-02")}
	token, err := m.token(ctx)
	if err != nil {
		return emailsending.DomainStats{}, err
	}
	var q queryRequest
	def := metricDefinition{Name: spamRateMetric}
	def.BaseMetric.StandardMetric = "SPAM_RATE"
	q.MetricDefinitions = []metricDefinition{def}
	q.TimeQuery.DateList.Dates = []googleDate{{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}}
	q.AggregationGranularity = "DAILY"
	body, err := json.Marshal(q)
	if err != nil {
		return emailsending.DomainStats{}, err
	}
	endpoint := m.base + "/v2/domains/" + url.PathEscape(normalized) + "/domainStats:query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return emailsending.DomainStats{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return emailsending.DomainStats{}, fmt.Errorf("%w: %v", ErrStatsRequest, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return emailsending.DomainStats{}, fmt.Errorf("%w: %v", ErrStatsRequest, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return stats, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return emailsending.DomainStats{}, fmt.Errorf("%w: status %d", ErrStatsRequest, resp.StatusCode)
	}
	var out queryResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return emailsending.DomainStats{}, fmt.Errorf("%w: decode: %v", ErrStatsRequest, err)
	}
	for _, s := range out.DomainStats {
		if s.Metric != spamRateMetric {
			continue
		}
		value := s.Value.DoubleValue
		if value == nil {
			value = s.Value.FloatValue
		}
		if value == nil {
			continue
		}
		stats.Available = true
		stats.SpamRatePerMil = int(math.Round(*value * 1000))
		return stats, nil
	}
	return stats, nil
}

func (m *Monitor) token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.accessToken != "" && m.now().Before(m.expiresAt) {
		return m.accessToken, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {m.clientID},
		"client_secret": {m.clientSecret},
		"refresh_token": {m.refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenRequest, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenRequest, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%w: status %d", ErrTokenRequest, resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("%w: no access token", ErrTokenRequest)
	}
	m.accessToken = out.AccessToken
	m.expiresAt = m.now().Add(time.Duration(out.ExpiresIn)*time.Second - tokenRefreshSlack)
	return m.accessToken, nil
}
