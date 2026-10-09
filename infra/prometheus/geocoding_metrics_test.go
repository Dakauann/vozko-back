package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/metrics"
)

func TestGeocodingMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.GeocodingMetricsRecorder = svc
	recorder.SetGeocodingBacklog("pending", 1200)
	recorder.AddGeocodingOutcomes("street", 40)
	recorder.AddGeocodingStale(2)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	out := string(body)
	for _, want := range []string{
		`app_geocoding_backlog{replica_id="replica-1",status="pending"} 1200`,
		`app_geocoding_outcomes_total{outcome="street",replica_id="replica-1"} 40`,
		`app_geocoding_stale_writes_total{replica_id="replica-1"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output misses %q\n---\n%s", want, out)
		}
	}
	for _, retired := range []string{"app_geocoding_provider", "app_geocoding_quota_hits", "app_geocoding_answers_reused"} {
		if strings.Contains(out, retired) {
			t.Fatalf("metrics output still exposes %q, which only the retired external provider fed", retired)
		}
	}
}

func TestGeocodingMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.SetGeocodingBacklog("pending", 1)
	svc.AddGeocodingOutcomes("street", 1)
	svc.AddGeocodingStale(1)
}
