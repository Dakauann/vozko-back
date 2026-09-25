package attendance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	attendancedomain "vozko/domain/attendance"
	"vozko/domain/cache"
	"vozko/infra/http/middleware"
)

type stubSections struct {
	attendancedomain.OverviewSectionsUseCase
	err      error
	lastRank string
}

func (s *stubSections) Summary(_ context.Context, _ string, _ attendancedomain.OverviewFilter) (*attendancedomain.SummarySection, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &attendancedomain.SummarySection{KPIs: attendancedomain.OverviewKPIs{Engaged: 7}}, nil
}

func (s *stubSections) Team(_ context.Context, _ string, filter attendancedomain.OverviewFilter) (*attendancedomain.TeamSection, error) {
	s.lastRank = filter.RankMetric
	return &attendancedomain.TeamSection{ByMember: []attendancedomain.MemberRow{}}, s.err
}

func serveSection(t *testing.T, sections *stubSections, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := &AttendanceHandler{sections: sections}
	router := mux.NewRouter()
	router.HandleFunc("/attendance/overview/{section}", h.GetOverviewSection)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestSectionEndpointAnswersWithOnlyThatSection(t *testing.T) {
	rec := serveSection(t, &stubSections{}, "/attendance/overview/summary?date_from=2026-09-01")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, ok := body["kpis"]; !ok {
		t.Fatalf("summary body has no kpis: %s", rec.Body.String())
	}
	if _, ok := body["by_member"]; ok {
		t.Fatalf("summary body leaked the team section: %s", rec.Body.String())
	}
}

func TestSectionEndpointPassesTheRankingMetricToTheTeam(t *testing.T) {
	sections := &stubSections{}
	rec := serveSection(t, sections, "/attendance/overview/team?rank_metric=volume")
	if rec.Code != http.StatusOK || sections.lastRank != "volume" {
		t.Fatalf("status = %d, rank = %q, want 200 and volume", rec.Code, sections.lastRank)
	}
}

func TestSectionEndpointRejectsAnUnknownSection(t *testing.T) {
	rec := serveSection(t, &stubSections{}, "/attendance/overview/live")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSectionEndpointTellsTheClientWhenToRetryABusyGate(t *testing.T) {
	rec := serveSection(t, &stubSections{err: cache.ErrGateBusy}, "/attendance/overview/summary")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("a busy gate answered without Retry-After")
	}
}

func TestSectionEndpointReportsATimeoutAsSuch(t *testing.T) {
	rec := serveSection(t, &stubSections{err: context.DeadlineExceeded}, "/attendance/overview/summary")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}

func TestSectionEndpointReportsOtherFailures(t *testing.T) {
	rec := serveSection(t, &stubSections{err: errors.New("boom")}, "/attendance/overview/summary")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestSectionEndpointWithoutAWiredServiceRefuses(t *testing.T) {
	h := &AttendanceHandler{}
	router := mux.NewRouter()
	router.HandleFunc("/attendance/overview/{section}", h.GetOverviewSection)
	req := httptest.NewRequest(http.MethodGet, "/attendance/overview/summary", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
