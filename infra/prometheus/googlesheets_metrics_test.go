package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/metrics"
)

func TestGoogleSheetsMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.GoogleSheetsMetricsRecorder = svc
	recorder.AddGoogleSheetsEvent("write", "written")
	recorder.ObserveGoogleSheetsBatch(12, 0.4)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	for _, want := range []string{
		`app_google_sheets_events_total{outcome="written",replica_id="replica-1",stage="write"} 1`,
		`app_google_sheets_batch_rows_count{replica_id="replica-1"} 1`,
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("metrics output misses %q", want)
		}
	}
}

func TestGoogleSheetsMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.AddGoogleSheetsEvent("write", "written")
	svc.ObserveGoogleSheetsBatch(1, 1)
}
