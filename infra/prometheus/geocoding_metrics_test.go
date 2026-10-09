package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/metrics"
)

func TestGeocodingMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.GeocodingMetricsRecorder = svc
	recorder.SetGeocodingBacklog("pending", 1200)
	recorder.AddGeocodingOutcomes("street", 40)
	recorder.ObserveGeocodingProvider("opencage", metrics.GeocodingProviderOK, 300*time.Millisecond)
	recorder.ObserveGeocodingProvider("opencage", metrics.GeocodingProviderError, time.Second)
	recorder.IncGeocodingQuotaHit(metrics.GeocodingQuotaDaily)
	recorder.AddGeocodingStale(2)
	recorder.IncGeocodingProviderPause("opencage", "key_rejected")
	recorder.IncGeocodingAnswerReused()
	recorder.IncGeocodingAnswerReused()

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	out := string(body)
	for _, want := range []string{
		`app_geocoding_backlog{replica_id="replica-1",status="pending"} 1200`,
		`app_geocoding_outcomes_total{outcome="street",replica_id="replica-1"} 40`,
		`app_geocoding_provider_seconds_count{provider="opencage",replica_id="replica-1",result="ok"} 1`,
		`app_geocoding_provider_errors_total{provider="opencage",replica_id="replica-1"} 1`,
		`app_geocoding_quota_hits_total{quota="daily",replica_id="replica-1"} 1`,
		`app_geocoding_stale_writes_total{replica_id="replica-1"} 2`,
		`app_geocoding_provider_pauses_total{provider="opencage",reason="key_rejected",replica_id="replica-1"} 1`,
		`app_geocoding_answers_reused_total{replica_id="replica-1"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output misses %q\n---\n%s", want, out)
		}
	}
}

func TestGeocodingMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.SetGeocodingBacklog("pending", 1)
	svc.AddGeocodingOutcomes("street", 1)
	svc.ObserveGeocodingProvider("opencage", "ok", time.Second)
	svc.IncGeocodingQuotaHit("daily")
	svc.AddGeocodingStale(1)
	svc.IncGeocodingProviderPause("opencage", "key_rejected")
	svc.IncGeocodingAnswerReused()
}

func TestGeocodingProviderPauseEndIsExposedAsATimestamp(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.GeocodingMetricsRecorder = svc
	recorder.SetGeocodingProviderPausedUntil("opencage", "key_rejected", time.Unix(1791500400, 0))

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	if want := `app_geocoding_provider_paused_until_seconds{provider="opencage",reason="key_rejected",replica_id="replica-1"} 1.7915004e+09`; !strings.Contains(string(body), want) {
		t.Fatalf("metrics output misses %q\n---\n%s", want, body)
	}
	var nilService *PrometheusService
	nilService.SetGeocodingProviderPausedUntil("opencage", "key_rejected", time.Now())
}
