package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/metrics"
)

func TestLeadImportMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.LeadImportMetricsRecorder = svc
	recorder.AddLeadImportRows(metrics.LeadImportRowsCreated, 4611)
	recorder.AddLeadImportRows(metrics.LeadImportRowsEnriched, 482)
	recorder.AddLeadImportRows(metrics.LeadImportRowsRejected, 71)
	recorder.AddLeadImportRows(metrics.LeadImportRowsSkipped, 0)
	recorder.AddLeadImportRuns(metrics.LeadImportDone, 1)
	recorder.AddLeadImportRuns("stalled", 2)
	recorder.ObserveLeadImportDuration(90 * time.Second)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	out := string(body)
	for _, want := range []string{
		`app_lead_import_rows_total{outcome="created",replica_id="replica-1"} 4611`,
		`app_lead_import_rows_total{outcome="enriched",replica_id="replica-1"} 482`,
		`app_lead_import_rows_total{outcome="rejected",replica_id="replica-1"} 71`,
		`app_lead_import_runs_total{outcome="done",replica_id="replica-1"} 1`,
		`app_lead_import_runs_total{outcome="stalled",replica_id="replica-1"} 2`,
		`app_lead_import_duration_seconds_count{replica_id="replica-1"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output misses %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, `outcome="skipped"`) {
		t.Fatal("an empty add created a series")
	}
}

func TestLeadImportMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.AddLeadImportRows(metrics.LeadImportRowsCreated, 1)
	svc.AddLeadImportRuns(metrics.LeadImportDone, 1)
	svc.ObserveLeadImportDuration(time.Second)
}
