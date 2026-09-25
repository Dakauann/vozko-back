package campaignreport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/cache"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_domain "vozko/domain/workspace"
)

const period = "date_from=2026-09-01&date_to=2026-09-07"

type reportsStub struct {
	err        error
	access     wc.ReportAccess
	period     wc.ReportPeriod
	calledWith string
}

func (s *reportsStub) record(name string, access wc.ReportAccess, period wc.ReportPeriod) {
	s.calledWith = name
	s.access = access
	s.period = period
}

func (s *reportsStub) Summary(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportSummary, error) {
	s.record("summary", access, period)
	return &wc.ReportSummary{CampaignID: access.CampaignID}, s.err
}

func (s *reportsStub) Daily(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportDaily, error) {
	s.record("daily", access, period)
	return &wc.ReportDaily{From: period.DateFrom}, s.err
}

func (s *reportsStub) Failures(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportFailures, error) {
	s.record("failures", access, period)
	return &wc.ReportFailures{}, s.err
}

func (s *reportsStub) Tags(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportTags, error) {
	s.record("tags", access, period)
	return &wc.ReportTags{}, s.err
}

func (s *reportsStub) Campaigns(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportCampaigns, error) {
	s.record("campaigns", access, period)
	return &wc.ReportCampaigns{}, s.err
}

type permission struct {
	resource workspace_domain.Resource
	action   workspace_domain.Action
}

type accessRecorder struct{ checked []permission }

func (a *accessRecorder) ac(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
	a.checked = append(a.checked, permission{resource, action})
	return next
}

func serve(t *testing.T, reports *reportsStub, target string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(reports), (&accessRecorder{}).ac)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-Workspace-ID", "ws1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestTheReportIsAnAttendanceSectionThatAlsoNeedsCampaignAccess(t *testing.T) {
	recorder := &accessRecorder{}
	RegisterRoutes(mux.NewRouter(), NewHandler(&reportsStub{}), recorder.ac)
	// It sits on the attendance page, so it follows the attendance gate; it shows campaign data, so it keeps the
	// campaign gate too. Nobody sees more than before the move.
	want := map[permission]bool{
		{workspace_domain.ResourceAttendance, workspace_domain.ActionRead}:       true,
		{workspace_domain.ResourceWhatsAppCampaigns, workspace_domain.ActionRead}: true,
	}
	if len(recorder.checked) != len(want) {
		t.Fatalf("checked = %v, want both attendance:read and whatsapp_campaigns:read", recorder.checked)
	}
	for _, p := range recorder.checked {
		if !want[p] {
			t.Fatalf("unexpected permission %v", p)
		}
	}
}

func TestEachCampaignSectionIsServedByItsOwnRoute(t *testing.T) {
	for _, section := range []string{"summary", "daily", "failures", "tags"} {
		t.Run(section, func(t *testing.T) {
			reports := &reportsStub{}

			rec := serve(t, reports, "/attendance/campaigns/"+section+"?"+period+"&campaign_id=c1")

			if rec.Code != http.StatusOK || reports.calledWith != section {
				t.Fatalf("status = %d, called %q; want 200 from %q", rec.Code, reports.calledWith, section)
			}
			if reports.access.WorkspaceID != "ws1" || reports.access.CampaignID != "c1" || reports.access.AllowDepartment == nil {
				t.Fatalf("access = %+v, want the request's workspace, campaign and a department decision", reports.access)
			}
		})
	}
}

func TestTheAllCampaignsViewHasNoCampaign(t *testing.T) {
	for _, section := range []string{"summary", "campaigns"} {
		t.Run(section, func(t *testing.T) {
			reports := &reportsStub{}

			rec := serve(t, reports, "/attendance/campaigns/"+section+"?"+period)

			if rec.Code != http.StatusOK || reports.calledWith != section || reports.access.CampaignID != "" {
				t.Fatalf("status = %d, called %q, access %+v; want the workspace view", rec.Code, reports.calledWith, reports.access)
			}
		})
	}
}

func TestThePeriodTravelsAsDaysAndTheServerPicksTheZone(t *testing.T) {
	reports := &reportsStub{}

	rec := serve(t, reports, "/attendance/campaigns/summary?"+period+"&timezone=Mars/Olympus")

	// The viewer's zone is not an input any more: attendance counts days in the workspace schedule's zone.
	if rec.Code != http.StatusOK || reports.period != (wc.ReportPeriod{DateFrom: "2026-09-01", DateTo: "2026-09-07"}) {
		t.Fatalf("status = %d, period = %+v", rec.Code, reports.period)
	}
}

func TestAnUnknownSectionIsNotFound(t *testing.T) {
	reports := &reportsStub{}

	if rec := serve(t, reports, "/attendance/campaigns/everything?"+period); rec.Code != http.StatusNotFound || reports.calledWith != "" {
		t.Fatalf("status = %d, called %q; want 404 and no read", rec.Code, reports.calledWith)
	}
}

func TestErrorsMapToTheirStatus(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{wc.ErrCampaignNotFound, http.StatusNotFound},
		{wc.ErrCampaignAccessDenied, http.StatusForbidden},
		{wce.ErrReportScopeInvalid, http.StatusBadRequest},
		{wce.ErrReportWindowInvalid, http.StatusBadRequest},
		{wce.ErrReportWindowTooLong, http.StatusBadRequest},
		{cache.ErrGateBusy, http.StatusServiceUnavailable},
		{context.DeadlineExceeded, http.StatusGatewayTimeout},
		{errors.New("pq: relation does not exist"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			rec := serve(t, &reportsStub{err: tc.err}, "/attendance/campaigns/summary?"+period+"&campaign_id=c1")

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if strings.Contains(rec.Body.String(), "pq:") {
				t.Fatalf("body %s leaks the database error", rec.Body.String())
			}
		})
	}
}

func TestTheSummaryIsWrittenAsTheSuccessPayload(t *testing.T) {
	rec := serve(t, &reportsStub{}, "/attendance/campaigns/summary?"+period+"&campaign_id=c1")

	var body wc.ReportSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body.CampaignID != "c1" {
		t.Fatalf("payload = %s, want the summary", rec.Body.String())
	}
}

func TestTheAllCampaignsViewNarrowsToTheRequestedDepartment(t *testing.T) {
	reports := &reportsStub{}

	rec := serve(t, reports, "/attendance/campaigns/summary?"+period+"&department_id=d1")

	if rec.Code != http.StatusOK || len(reports.access.DepartmentIDs) != 1 || reports.access.DepartmentIDs[0] != "d1" {
		t.Fatalf("status = %d, departments = %v; want only d1", rec.Code, reports.access.DepartmentIDs)
	}
}
