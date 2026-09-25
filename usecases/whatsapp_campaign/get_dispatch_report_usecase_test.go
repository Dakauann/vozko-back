package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/cache"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type reportCampaignFinderStub struct {
	campaign *wc.Campaign
	err      error
}

func (s reportCampaignFinderStub) FindByID(string) (*wc.Campaign, error) {
	return s.campaign, s.err
}

type reportReaderStub struct {
	funnel    wce.Funnel
	daily     []wce.DayCount
	fail      error
	reads     []string
	scopes    []wce.ReportScope
	tagLimit  int
	rowLimit  int
	windowArg wce.DayWindow
}

func (s *reportReaderStub) read(name string, scope wce.ReportScope) error {
	s.reads = append(s.reads, name)
	s.scopes = append(s.scopes, scope)
	return s.fail
}

func (s *reportReaderStub) Funnel(scope wce.ReportScope) (wce.Funnel, error) {
	return s.funnel, s.read("funnel", scope)
}

func (s *reportReaderStub) Daily(scope wce.ReportScope, window wce.DayWindow) ([]wce.DayCount, error) {
	s.windowArg = window
	return s.daily, s.read("daily", scope)
}

func (s *reportReaderStub) FailureReasons(scope wce.ReportScope) ([]wce.FailureReasonCount, error) {
	return nil, s.read("reasons", scope)
}

func (s *reportReaderStub) Tags(scope wce.ReportScope, limit int) ([]wce.TagCount, error) {
	s.tagLimit = limit
	return nil, s.read("tags", scope)
}

func (s *reportReaderStub) Campaigns(scope wce.ReportScope, limit int) ([]wce.CampaignFunnel, error) {
	s.rowLimit = limit
	return nil, s.read("campaigns", scope)
}

type keyRecordingMemo struct {
	keys []string
	ttls []time.Duration
}

func (m *keyRecordingMemo) Remember(ctx context.Context, key string, ttl time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	m.keys = append(m.keys, key)
	m.ttls = append(m.ttls, ttl)
	return compute(ctx)
}

type busyGate struct{}

func (busyGate) Acquire(context.Context) (func(), error) { return nil, cache.ErrGateBusy }

func reportPeriod() wc.ReportPeriod {
	return wc.ReportPeriod{DateFrom: "2026-09-01", DateTo: "2026-09-02"}
}

type locatorStub struct {
	loc         *time.Location
	err         error
	departments []string
}

func (s *locatorStub) Location(_ context.Context, _ string, departmentID string) (*time.Location, error) {
	s.departments = append(s.departments, departmentID)
	return s.loc, s.err
}

func utc() *locatorStub { return &locatorStub{loc: time.UTC} }

func ownedCampaign() *wc.Campaign {
	return &wc.Campaign{
		ID:           "c1",
		WorkspaceID:  "ws1",
		DepartmentID: "d1",
		Name:         "Lançamento",
		TemplateName: "oferta",
		CreatedAt:    time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}
}

func allowAll(string) bool { return true }

func campaignAccess() wc.ReportAccess {
	return wc.ReportAccess{WorkspaceID: "ws1", CampaignID: "c1", AllowDepartment: allowAll}
}

func workspaceAccess() wc.ReportAccess {
	return wc.ReportAccess{WorkspaceID: "ws1", DepartmentIDs: []string{"d1"}, AllowDepartment: allowAll}
}

func newReportUseCase(reader *reportReaderStub, memo cache.Memo, gate cache.Gate) wc.GetDispatchReportUseCase {
	return NewGetDispatchReportUseCase(reportCampaignFinderStub{campaign: ownedCampaign()}, reader, utc(), ReportCaching{
		Memo: memo, Gate: gate, TTL: time.Minute,
	})
}

type sectionCall func(uc wc.GetDispatchReportUseCase, access wc.ReportAccess, period wc.ReportPeriod) error

func sharedSections() map[string]sectionCall {
	return map[string]sectionCall{
		"summary": func(uc wc.GetDispatchReportUseCase, a wc.ReportAccess, p wc.ReportPeriod) error {
			_, err := uc.Summary(context.Background(), a, p)
			return err
		},
		"daily": func(uc wc.GetDispatchReportUseCase, a wc.ReportAccess, p wc.ReportPeriod) error {
			_, err := uc.Daily(context.Background(), a, p)
			return err
		},
		"failures": func(uc wc.GetDispatchReportUseCase, a wc.ReportAccess, p wc.ReportPeriod) error {
			_, err := uc.Failures(context.Background(), a, p)
			return err
		},
		"tags": func(uc wc.GetDispatchReportUseCase, a wc.ReportAccess, p wc.ReportPeriod) error {
			_, err := uc.Tags(context.Background(), a, p)
			return err
		},
	}
}

func TestACampaignSummaryCarriesTheCampaignAndItsFunnel(t *testing.T) {
	reader := &reportReaderStub{funnel: wce.Funnel{Base: 10, Sent: 9, Delivered: 8, Read: 6, Replied: 3}}

	got, err := newReportUseCase(reader, nil, nil).Summary(context.Background(), campaignAccess(), reportPeriod())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}

	created := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	want := &wc.ReportSummary{
		CampaignID: "c1", CampaignName: "Lançamento", TemplateName: "oferta", CampaignCreatedAt: &created,
		Funnel: reader.funnel,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Summary() = %+v, want %+v", got, want)
	}
	if !reader.scopes[0].IsCampaign() || reader.scopes[0].CampaignID != "c1" {
		t.Fatalf("scope = %+v, want the campaign", reader.scopes[0])
	}
}

func TestAWorkspaceSummaryCoversTheCallersDispatchCampaignsInThePeriod(t *testing.T) {
	reader := &reportReaderStub{funnel: wce.Funnel{Base: 40}}
	got, err := newReportUseCase(reader, nil, nil).Summary(context.Background(), workspaceAccess(), reportPeriod())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}

	if got.CampaignID != "" || got.CampaignCreatedAt != nil || got.Funnel.Base != 40 {
		t.Fatalf("Summary() = %+v, want a workspace summary with no campaign", got)
	}
	scope := reader.scopes[0]
	if scope.IsCampaign() || scope.WorkspaceID != "ws1" || !reflect.DeepEqual(scope.DepartmentIDs, []string{"d1"}) {
		t.Fatalf("scope = %+v, want the caller's workspace and departments", scope)
	}
	if scope.ExcludedType != string(wc.CampaignTypeOrganic) || scope.Window.FirstDay() != "2026-09-01" {
		t.Fatalf("scope = %+v, want organic campaigns left out and the period applied", scope)
	}
}

func TestTheCampaignBreakdownIsAWorkspaceSection(t *testing.T) {
	reader := &reportReaderStub{}
	uc := newReportUseCase(reader, nil, nil)

	got, err := uc.Campaigns(context.Background(), workspaceAccess(), reportPeriod())
	if err != nil || got.Campaigns == nil || reader.rowLimit != wce.ReportCampaignLimit {
		t.Fatalf("Campaigns() = %+v, %v, limit %d", got, err, reader.rowLimit)
	}
	if _, err := uc.Campaigns(context.Background(), campaignAccess(), reportPeriod()); !errors.Is(err, wce.ErrReportScopeInvalid) {
		t.Fatalf("Campaigns(campaign) error = %v, want %v", err, wce.ErrReportScopeInvalid)
	}
}

func TestDailyFillsEveryDayOfTheWindow(t *testing.T) {
	reader := &reportReaderStub{daily: []wce.DayCount{{Day: "2026-09-02", Sent: 9}}}

	got, err := newReportUseCase(reader, nil, nil).Daily(context.Background(), campaignAccess(), reportPeriod())
	if err != nil {
		t.Fatalf("Daily() error = %v", err)
	}

	want := &wc.ReportDaily{
		From: "2026-09-01", To: "2026-09-02", Timezone: "UTC",
		Days: []wce.DayCount{{Day: "2026-09-01"}, {Day: "2026-09-02", Sent: 9}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Daily() = %+v, want %+v", got, want)
	}
}

func TestListSectionsAnswerEmptyListsRatherThanNull(t *testing.T) {
	uc := newReportUseCase(&reportReaderStub{}, nil, nil)

	failures, err := uc.Failures(context.Background(), campaignAccess(), reportPeriod())
	if err != nil || failures.Reasons == nil {
		t.Fatalf("Failures() = %+v, %v; want an empty list", failures, err)
	}
	tags, err := uc.Tags(context.Background(), campaignAccess(), reportPeriod())
	if err != nil || tags.Tags == nil {
		t.Fatalf("Tags() = %+v, %v; want an empty list", tags, err)
	}
}

func TestEverySectionRefusesBeforeTouchingTheCacheOrTheDatabase(t *testing.T) {
	denials := map[string]wc.ReportAccess{
		"another workspace's campaign":            {WorkspaceID: "ws2", CampaignID: "c1", AllowDepartment: allowAll},
		"a campaign in a department out of scope": {WorkspaceID: "ws1", CampaignID: "c1", AllowDepartment: func(string) bool { return false }},
		"no department decision":                  {WorkspaceID: "ws1", CampaignID: "c1"},
		"no workspace on the request":             {CampaignID: "c1", AllowDepartment: allowAll},
		"a workspace view with no department":     {WorkspaceID: "ws1", DepartmentsBlocked: true, AllowDepartment: allowAll},
	}
	for section, call := range sharedSections() {
		for name, access := range denials {
			t.Run(section+"/"+name, func(t *testing.T) {
				reader := &reportReaderStub{}
				memo := &keyRecordingMemo{}

				err := call(newReportUseCase(reader, memo, nil), access, reportPeriod())

				if !errors.Is(err, wc.ErrCampaignAccessDenied) {
					t.Fatalf("error = %v, want %v", err, wc.ErrCampaignAccessDenied)
				}
				if len(reader.reads) != 0 || len(memo.keys) != 0 {
					t.Fatalf("reads = %v, cache keys = %v; a refused caller must reach neither", reader.reads, memo.keys)
				}
			})
		}
	}
}

func TestAMissingCampaignIsReportedAsMissing(t *testing.T) {
	uc := NewGetDispatchReportUseCase(reportCampaignFinderStub{err: wc.ErrCampaignNotFound}, &reportReaderStub{}, utc(), ReportCaching{})

	if _, err := uc.Summary(context.Background(), campaignAccess(), reportPeriod()); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("Summary() error = %v, want %v", err, wc.ErrCampaignNotFound)
	}
}

func TestCacheKeysSeparateScopesSectionsAndPeriods(t *testing.T) {
	memo := &keyRecordingMemo{}
	uc := newReportUseCase(&reportReaderStub{}, memo, nil)
	august := wc.ReportPeriod{DateFrom: "2026-08-01", DateTo: "2026-08-31"}

	for _, call := range sharedSections() {
		if err := call(uc, campaignAccess(), reportPeriod()); err != nil {
			t.Fatal(err)
		}
		if err := call(uc, workspaceAccess(), reportPeriod()); err != nil {
			t.Fatal(err)
		}
		if err := call(uc, workspaceAccess(), august); err != nil {
			t.Fatal(err)
		}
	}

	seen := make(map[string]bool)
	for i, key := range memo.keys {
		if memo.ttls[i] != time.Minute {
			t.Fatalf("key %q ttl = %v, want the configured ttl", key, memo.ttls[i])
		}
		seen[key] = true
	}
	if len(seen) != 12 {
		t.Fatalf("keys = %v, want twelve distinct entries", memo.keys)
	}
	for _, key := range memo.keys {
		if strings.Contains(key, "campaign:c1") && strings.Contains(key, ":summary:") && strings.Contains(key, "2026-08") {
			t.Fatalf("campaign summary key %q depends on the period although the funnel covers the whole campaign", key)
		}
	}
}

func TestABusyAnalyticsGateSurfacesInsteadOfQueryingAnyway(t *testing.T) {
	for section, call := range sharedSections() {
		t.Run(section, func(t *testing.T) {
			reader := &reportReaderStub{}

			err := call(newReportUseCase(reader, nil, busyGate{}), campaignAccess(), reportPeriod())

			if !errors.Is(err, cache.ErrGateBusy) || len(reader.reads) != 0 {
				t.Fatalf("error = %v, reads = %v; want ErrGateBusy and no query", err, reader.reads)
			}
		})
	}
}

func TestAReaderFailureIsReturnedNotHidden(t *testing.T) {
	boom := errors.New("database unavailable")
	for section, call := range sharedSections() {
		t.Run(section, func(t *testing.T) {
			err := call(newReportUseCase(&reportReaderStub{fail: boom}, nil, nil), campaignAccess(), reportPeriod())
			if !errors.Is(err, boom) {
				t.Fatalf("error = %v, want %v", err, boom)
			}
		})
	}
}

func fortaleza(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Skip("tz database unavailable")
	}
	return loc
}

func TestThePeriodIsCountedInTheWorkspacesTimezone(t *testing.T) {
	loc := fortaleza(t)
	locator := &locatorStub{loc: loc}
	reader := &reportReaderStub{}
	uc := NewGetDispatchReportUseCase(reportCampaignFinderStub{campaign: ownedCampaign()}, reader, locator, ReportCaching{})

	daily, err := uc.Daily(context.Background(), workspaceAccess(), reportPeriod())
	if err != nil {
		t.Fatal(err)
	}
	// Attendance counts days in the workspace schedule's zone; the report next to it must cut days the same way,
	// never in whatever zone the viewer's browser happens to be in.
	if daily.Timezone != "America/Fortaleza" || reader.windowArg.Location != loc || reader.scopes[0].Window.Location != loc {
		t.Fatalf("daily timezone = %q, window zone = %v", daily.Timezone, reader.windowArg.Location)
	}
	if locator.departments[0] != "d1" {
		t.Fatalf("zone resolved for department %q, want the one department in view (d1)", locator.departments[0])
	}
}

func TestTheZoneFollowsTheDepartmentBeingLookedAt(t *testing.T) {
	cases := map[string]struct {
		access wc.ReportAccess
		want   string
	}{
		"a campaign uses its own department":          {campaignAccess(), "d1"},
		"one department uses that department":         {workspaceAccess(), "d1"},
		"several departments use the workspace zone":  {wc.ReportAccess{WorkspaceID: "ws1", DepartmentIDs: []string{"d1", "d2"}, AllowDepartment: allowAll}, ""},
		"the whole workspace uses the workspace zone": {wc.ReportAccess{WorkspaceID: "ws1", AllowDepartment: allowAll}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			locator := utc()
			uc := NewGetDispatchReportUseCase(reportCampaignFinderStub{campaign: ownedCampaign()}, &reportReaderStub{}, locator, ReportCaching{})
			if _, err := uc.Summary(context.Background(), tc.access, reportPeriod()); err != nil {
				t.Fatal(err)
			}
			if locator.departments[0] != tc.want {
				t.Fatalf("department = %q, want %q", locator.departments[0], tc.want)
			}
		})
	}
}

func TestAPeriodItCannotCutIsRejectedBeforeAnyRead(t *testing.T) {
	cases := map[string]struct {
		period wc.ReportPeriod
		want   error
	}{
		"no period at all": {wc.ReportPeriod{}, wce.ErrReportWindowInvalid},
		"a malformed day":  {wc.ReportPeriod{DateFrom: "01/09/2026", DateTo: "2026-09-07"}, wce.ErrReportWindowInvalid},
		"a reversed range": {wc.ReportPeriod{DateFrom: "2026-09-07", DateTo: "2026-09-01"}, wce.ErrReportWindowInvalid},
		"too many days":    {wc.ReportPeriod{DateFrom: "2026-01-01", DateTo: "2026-09-07"}, wce.ErrReportWindowTooLong},
	}
	for section, call := range sharedSections() {
		for name, tc := range cases {
			t.Run(section+"/"+name, func(t *testing.T) {
				reader := &reportReaderStub{}
				memo := &keyRecordingMemo{}
				err := call(newReportUseCase(reader, memo, nil), workspaceAccess(), tc.period)
				if !errors.Is(err, tc.want) || len(reader.reads) != 0 || len(memo.keys) != 0 {
					t.Fatalf("error = %v reads = %v keys = %v; want %v before any read", err, reader.reads, memo.keys, tc.want)
				}
			})
		}
	}
}

func TestAZoneThatCannotBeResolvedRefusesInsteadOfGuessing(t *testing.T) {
	boom := errors.New("config unavailable")
	for name, locator := range map[string]wc.ReportLocator{
		"lookup fails":     &locatorStub{err: boom},
		"no zone returned": &locatorStub{},
		"no locator wired": nil,
	} {
		t.Run(name, func(t *testing.T) {
			reader := &reportReaderStub{}
			uc := NewGetDispatchReportUseCase(reportCampaignFinderStub{campaign: ownedCampaign()}, reader, locator, ReportCaching{})
			if _, err := uc.Daily(context.Background(), workspaceAccess(), reportPeriod()); err == nil || len(reader.reads) != 0 {
				t.Fatalf("error = %v reads = %v; a guessed zone would cut days wrong", err, reader.reads)
			}
		})
	}
}
