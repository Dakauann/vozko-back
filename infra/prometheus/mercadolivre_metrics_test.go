package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/metrics"
)

func TestMercadoLivreEventsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.MercadoLivreMetricsRecorder = svc
	recorder.AddMercadoLivreEvent("notify", "refused_token")
	recorder.AddMercadoLivreEvent("notify", "refused_token")

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	want := `app_mercadolivre_events_total{outcome="refused_token",replica_id="replica-1",stage="notify"} 2`
	if !strings.Contains(string(body), want) {
		t.Fatalf("metrics output misses %q\n---\n%s", want, body)
	}
}

func TestMercadoLivreMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.AddMercadoLivreEvent("notify", "accepted")
}
