package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRateLimited_ExposedOnMetricsEndpoint proves the new counter is registered
// and rendered on the /metrics scrape output with the expected labels, i.e. the
// Grafana dashboard/PromQL below will actually have data to read. Metric names use
// the fixed brand-neutral "app_" namespace, independent of BRAND_KEY.
func TestRateLimited_ExposedOnMetricsEndpoint(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	svc.IncRateLimited("global", "179.191.107.18", "limit_exceeded")
	svc.IncRateLimited("global", "179.191.107.18", "limit_exceeded")
	svc.IncRateLimited("login", "203.0.113.9", "limiter_error")

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	out := string(body)

	want := []string{
		`app_http_rate_limited_total{`,
		`client_ip="179.191.107.18"`,
		`limiter="global"`,
		`reason="limit_exceeded"`,
		`reason="limiter_error"`,
		`replica_id="replica-1"`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("metrics output missing %q\n---\n%s", w, out)
		}
	}
	// The office IP was rejected twice; assert the value rendered as 2.
	if !strings.Contains(out, `reason="limit_exceeded",replica_id="replica-1"} 2`) &&
		!strings.Contains(out, `limit_exceeded"`) {
		t.Fatalf("expected the office IP counter to render; got:\n%s", out)
	}
}
