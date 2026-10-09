package opencage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

type fakeLimiter struct {
	allowed    bool
	retryAfter time.Duration
	err        error
	calls      int
}

func (f *fakeLimiter) Allow(string) (bool, time.Duration, error) {
	f.calls++
	return f.allowed, f.retryAfter, f.err
}

var fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func paulista() geo.Query {
	return geo.Query{WorkspaceID: "ws-1", Postal: address.Postal{
		ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP",
	}}
}

func newTestClient(t *testing.T, handler http.HandlerFunc, limiter *fakeLimiter) (*Client, *atomic.Int32, *[]url.Values) {
	t.Helper()
	var hits atomic.Int32
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		queries = append(queries, r.URL.Query())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	if limiter == nil {
		limiter = &fakeLimiter{allowed: true}
	}
	c, err := NewClient(Config{APIKey: "secret", BaseURL: srv.URL, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	c.now = func() time.Time { return fixedNow }
	return c, &hits, &queries
}

func answer(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func result(kind string, lat, lng float64, country string) string {
	return `{"components":{"_type":"` + kind + `","country_code":"` + country + `"},"geometry":{"lat":` +
		strconv.FormatFloat(lat, 'f', -1, 64) + `,"lng":` + strconv.FormatFloat(lng, 'f', -1, 64) + `},"confidence":9}`
}

func itoa(v int) string { return strconv.Itoa(v) }

func ok(results ...string) string {
	return `{"results":[` + strings.Join(results, ",") + `],"status":{"code":200,"message":"OK"},"total_results":` + itoa(len(results)) + `}`
}

func TestNewClientRefusesAMissingKeyOrLimiter(t *testing.T) {
	if _, err := NewClient(Config{Limiter: &fakeLimiter{}}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("NewClient() without a key err = %v, want ErrNotConfigured", err)
	}
	if _, err := NewClient(Config{APIKey: "k"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("NewClient() without a limiter err = %v, want ErrNotConfigured", err)
	}
}

func TestGeocodeMapsTheResultTypeToAPrecision(t *testing.T) {
	tests := []struct {
		kind string
		want geo.Precision
	}{
		{"building", geo.PrecisionAddress},
		{"road", geo.PrecisionStreet},
		{"postcode", geo.PrecisionPostalCode},
		{"neighbourhood", geo.PrecisionDistrict},
		{"suburb", geo.PrecisionDistrict},
		{"city", geo.PrecisionCity},
		{"town", geo.PrecisionCity},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			c, _, _ := newTestClient(t, answer(ok(result(tt.kind, -23.5613, -46.6565, "br"))), nil)
			got, err := c.Geocode(context.Background(), paulista())
			if err != nil {
				t.Fatalf("Geocode() err = %v", err)
			}
			want := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: tt.want, Source: geo.SourceProvider, Provider: ProviderName, FixedAt: fixedNow}
			if got.Kind != geo.OutcomeLocated || got.Fix != want {
				t.Fatalf("Geocode() = %+v, want located %+v", got, want)
			}
		})
	}
}

func TestGeocodeSendsAPrivateBrazilianQuery(t *testing.T) {
	c, _, queries := newTestClient(t, answer(ok(result("building", -23.5613, -46.6565, "br"))), nil)
	if _, err := c.Geocode(context.Background(), paulista()); err != nil {
		t.Fatal(err)
	}
	q := (*queries)[0]
	if q.Get("key") != "secret" || q.Get("countrycode") != "br" || q.Get("no_record") != "1" || q.Get("no_annotations") != "1" || q.Get("language") != "pt" {
		t.Fatalf("query = %v, want the key, Brazil only, no record kept and no annotations", q)
	}
	if want := "Avenida Paulista, 1000, Bela Vista, São Paulo - SP, 01310-100, Brasil"; q.Get("q") != want {
		t.Fatalf("q = %q, want %q", q.Get("q"), want)
	}
}

func TestGeocodeNeverStoresAnAnswerOutsideBrazilOrTooVague(t *testing.T) {
	for name, body := range map[string]string{
		"no results":        ok(),
		"another country":   ok(result("building", -34.6, -58.38, "ar")),
		"a whole state":     ok(result("state", -22.0, -48.0, "br")),
		"a point not in br": ok(result("building", 40.7, -74.0, "br")),
	} {
		t.Run(name, func(t *testing.T) {
			c, _, _ := newTestClient(t, answer(body), nil)
			got, err := c.Geocode(context.Background(), paulista())
			if err != nil || got.Kind != geo.OutcomeNotFound {
				t.Fatalf("Geocode() = %+v, %v, want not found", got, err)
			}
		})
	}
}

func TestGeocodeCallsTwoDistantHousesAmbiguous(t *testing.T) {
	c, _, _ := newTestClient(t, answer(ok(result("building", -23.5613, -46.6565, "br"), result("building", -22.9068, -43.1729, "br"))), nil)
	got, err := c.Geocode(context.Background(), paulista())
	if err != nil || got.Kind != geo.OutcomeAmbiguous {
		t.Fatalf("Geocode() = %+v, %v, want ambiguous", got, err)
	}
	near, _, _ := newTestClient(t, answer(ok(result("building", -23.5613, -46.6565, "br"), result("road", -23.5620, -46.6570, "br"))), nil)
	if got, _ := near.Geocode(context.Background(), paulista()); got.Kind != geo.OutcomeLocated || got.Fix.Precision != geo.PrecisionAddress {
		t.Fatalf("two close answers = %+v, want the first one located", got)
	}
}

func TestGeocodeMakesOneBilledRequestPerQueryAndLeavesRetriesToTheSweeper(t *testing.T) {
	var n atomic.Int32
	limiter := &fakeLimiter{allowed: true}
	c, hits, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		answer(ok(result("road", -23.5613, -46.6565, "br")))(w, r)
	}, limiter)
	got, err := c.Geocode(context.Background(), paulista())
	if err != nil || got.Kind != geo.OutcomeUnavailable || got.Reason != geo.ReasonProviderDown {
		t.Fatalf("Geocode() = %+v, %v, want unavailable so the sweeper backs off", got, err)
	}
	if hits.Load() != 1 || limiter.calls != 1 {
		t.Fatalf("calls = %d, limiter asks = %d, want one request under the one usage slot the chain took", hits.Load(), limiter.calls)
	}
}

func TestGeocodeFailuresLeaveTheAddressWaiting(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		reason geo.UnavailableReason
		hits   int32
		wait   time.Duration
	}{
		{"the service stays down", http.StatusServiceUnavailable, `{}`, geo.ReasonProviderDown, 1, 0},
		{"too many requests", http.StatusTooManyRequests, `{"status":{"code":429,"message":"rate"}}`, geo.ReasonRateLimited, 1, 0},
		{"the account quota is spent until the reset", http.StatusPaymentRequired, `{"status":{"code":402,"message":"quota"},"rate":{"reset":` + itoa(int(fixedNow.Add(5*time.Hour).Unix())) + `}}`, geo.ReasonAccountQuotaSpent, 1, 5 * time.Hour},
		{"the key was refused", http.StatusUnauthorized, `{"status":{"code":401,"message":"invalid key"}}`, geo.ReasonKeyRejected, 1, 0},
		{"the key was disabled", http.StatusForbidden, `{"status":{"code":403,"message":"disabled"}}`, geo.ReasonKeyDisabled, 1, 0},
		{"the query text was refused", http.StatusBadRequest, `{"status":{"code":400,"message":"invalid request"}}`, geo.ReasonQueryRefused, 1, 0},
		{"the query text was too long", http.StatusGone, `{"status":{"code":410,"message":"request too long"}}`, geo.ReasonQueryRefused, 1, 0},
		{"an unknown endpoint refuses the account", http.StatusNotFound, `{}`, geo.ReasonAccountRefused, 1, 0},
		{"an unreadable answer", http.StatusOK, `not json`, geo.ReasonInvalidAnswer, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, hits, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}, nil)
			got, err := c.Geocode(context.Background(), paulista())
			if err != nil {
				t.Fatalf("Geocode() err = %v, want an outcome", err)
			}
			if got.Kind != geo.OutcomeUnavailable || got.Reason != tt.reason || got.RetryAfter != tt.wait {
				t.Fatalf("Geocode() = %+v, want unavailable %q waiting %v", got, tt.reason, tt.wait)
			}
			if hits.Load() != tt.hits {
				t.Fatalf("calls = %d, want %d", hits.Load(), tt.hits)
			}
		})
	}
}

func TestGeocodeRefusesWhenTheLimiterSaysNoOrFails(t *testing.T) {
	tests := []struct {
		name    string
		limiter *fakeLimiter
		reason  geo.UnavailableReason
		wait    time.Duration
	}{
		{"a long wait is handed back", &fakeLimiter{allowed: false, retryAfter: time.Minute}, geo.ReasonRateLimited, time.Minute},
		{"a limiter error refuses", &fakeLimiter{err: errors.New("redis down")}, geo.ReasonRateLimiterDown, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, hits, _ := newTestClient(t, answer(ok(result("building", -23.5613, -46.6565, "br"))), tt.limiter)
			got, err := c.Geocode(context.Background(), paulista())
			if err != nil || got.Kind != geo.OutcomeUnavailable || got.Reason != tt.reason || got.RetryAfter != tt.wait {
				t.Fatalf("Geocode() = %+v, %v, want unavailable %q", got, err, tt.reason)
			}
			if hits.Load() != 0 {
				t.Fatalf("the provider was called %d times without a slot from the limiter", hits.Load())
			}
		})
	}
}

func TestGeocodeWaitsOutAShortLimiterWindow(t *testing.T) {
	limiter := &shortWindow{}
	c, hits, _ := newTestClient(t, answer(ok(result("building", -23.5613, -46.6565, "br"))), nil)
	c.limiter = limiter
	got, err := c.Geocode(context.Background(), paulista())
	if err != nil || got.Kind != geo.OutcomeLocated || hits.Load() != 1 || limiter.calls != 2 {
		t.Fatalf("Geocode() = %+v, %v, %d calls, %d limiter asks, want one wait then the call", got, err, hits.Load(), limiter.calls)
	}
}

type shortWindow struct{ calls int }

func (s *shortWindow) Allow(string) (bool, time.Duration, error) {
	s.calls++
	if s.calls == 1 {
		return false, time.Second, nil
	}
	return true, 0, nil
}

func TestGeocodeRefusesAnEmptyQuery(t *testing.T) {
	c, hits, _ := newTestClient(t, answer(ok()), nil)
	got, err := c.Geocode(context.Background(), geo.Query{})
	if err != nil || got.Kind != geo.OutcomeNotFound || hits.Load() != 0 {
		t.Fatalf("Geocode(empty) = %+v, %v, %d calls, want not found without a call", got, err, hits.Load())
	}
}
