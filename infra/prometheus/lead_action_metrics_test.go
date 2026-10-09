package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/metrics"
)

func TestLeadActionMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.LeadActionMetricsRecorder = svc
	recorder.AddLeadActionRuns("classify", metrics.LeadActionDone, metrics.LeadActionNoFailure, 1)
	recorder.AddLeadActionRuns("block", metrics.LeadActionFailed, "stalled", 2)
	recorder.AddLeadActionRuns("send_template", metrics.LeadActionPrepared, metrics.LeadActionNoFailure, 0)
	recorder.ObserveLeadActionDuration("classify", metrics.LeadActionDone, 42*time.Second)
	recorder.AddLeadActionSkips("classify", "unchanged", 600)
	recorder.AddLeadActionSkips("send_template", "opted_out", 0)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	out := string(body)
	for _, want := range []string{
		`app_lead_action_runs_total{action="classify",failure="none",replica_id="replica-1",status="done"} 1`,
		`app_lead_action_runs_total{action="block",failure="stalled",replica_id="replica-1",status="failed"} 2`,
		`app_lead_action_duration_seconds_count{action="classify",replica_id="replica-1",status="done"} 1`,
		`app_lead_action_skips_total{action="classify",reason="unchanged",replica_id="replica-1"} 600`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output misses %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, `status="prepared"`) || strings.Contains(out, `reason="opted_out"`) {
		t.Fatal("an empty add created a series")
	}
}

func TestLeadActionDurationsSplitRequestsThatTakeTensOfMilliseconds(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	svc.ObserveLeadActionDuration("export", metrics.LeadActionCreated, 40*time.Millisecond)
	svc.ObserveLeadActionDuration("export", metrics.LeadActionCreated, 180*time.Millisecond)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	for _, want := range []string{
		`app_lead_action_duration_seconds_bucket{action="export",replica_id="replica-1",status="created",le="0.05"} 1`,
		`app_lead_action_duration_seconds_bucket{action="export",replica_id="replica-1",status="created",le="0.1"} 1`,
		`app_lead_action_duration_seconds_bucket{action="export",replica_id="replica-1",status="created",le="0.25"} 2`,
		`app_lead_action_duration_seconds_bucket{action="export",replica_id="replica-1",status="created",le="7200"} 2`,
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("metrics output misses %q\n---\n%s", want, body)
		}
	}
}

func TestLeadActionMetricsLabelAnEmptyValue(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	svc.AddLeadActionRuns("", metrics.LeadActionFailed, "", 1)

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	if want := `app_lead_action_runs_total{action="_",failure="_",replica_id="replica-1",status="failed"} 1`; !strings.Contains(string(body), want) {
		t.Fatalf("metrics output misses %q\n---\n%s", want, body)
	}
}

func TestLeadActionMetricsToleratesANilService(t *testing.T) {
	var svc *PrometheusService
	svc.AddLeadActionRuns("classify", metrics.LeadActionDone, metrics.LeadActionNoFailure, 1)
	svc.ObserveLeadActionDuration("classify", metrics.LeadActionDone, time.Second)
	svc.AddLeadActionSkips("classify", "gone", 1)
}
