package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/metrics"
)

func TestTikTokEventsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.TikTokMetricsRecorder = svc
	recorder.AddTikTokEvent("webhook", "accepted")
	recorder.AddTikTokEvent("webhook", "accepted")

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	want := `app_tiktok_events_total{outcome="accepted",replica_id="replica-1",stage="webhook"} 2`
	if !strings.Contains(string(body), want) {
		t.Fatalf("metrics output misses %q\n---\n%s", want, body)
	}
}

func TestTikTokMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.AddTikTokEvent("webhook", "accepted")
}
