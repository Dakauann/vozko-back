package livedecision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ld "vozko/domain/livedecision"
	livedecisions_usecase "vozko/usecases/livedecisions"
)

type summaryLog struct{}

func (summaryLog) Append(context.Context, ld.Record) error { return nil }
func (summaryLog) Summarize(context.Context, time.Time) ([]ld.Summary, error) {
	return []ld.Summary{{WorkspaceID: "ws", WorkspaceName: "Escola", Decisions: 3, AvgLatency: 250 * time.Millisecond, Effects: map[string]int{"stage_moved": 2}}}, nil
}

func TestTheSummaryReportsLatencyInMilliseconds(t *testing.T) {
	h := NewHandler(livedecisions_usecase.NewService(livedecisions_usecase.Deps{Log: summaryLog{}}))
	rec := httptest.NewRecorder()
	h.Summary(rec, httptest.NewRequest(http.MethodGet, "/admin/live-decisions/summary?days=30", nil))
	var got []SummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	if rec.Code != http.StatusOK || len(got) != 1 || got[0].AvgLatencyMs != 250 || got[0].Effects["stage_moved"] != 2 {
		t.Fatalf("summary: %d %+v", rec.Code, got)
	}
}
