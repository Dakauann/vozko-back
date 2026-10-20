package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type sinkRecorder struct {
	calls     []string
	summary   *wc.ReportSummary
	daily     *wc.ReportDaily
	campaigns int
	failures  *wc.ReportFailures
	tags      int
	failOn    string
}

var errSinkStopped = errors.New("the sheet refused")

func (s *sinkRecorder) call(name string) error {
	s.calls = append(s.calls, name)
	if s.failOn == name {
		return errSinkStopped
	}
	return nil
}

func (s *sinkRecorder) Summary(summary *wc.ReportSummary) error {
	s.summary = summary
	return s.call("summary")
}

func (s *sinkRecorder) Daily(daily *wc.ReportDaily) error {
	s.daily = daily
	return s.call("daily")
}

func (s *sinkRecorder) Campaigns(rows []wce.CampaignFunnel) error {
	s.campaigns += len(rows)
	return s.call("campaigns")
}

func (s *sinkRecorder) Failures(failures *wc.ReportFailures) error {
	s.failures = failures
	return s.call("failures")
}

func (s *sinkRecorder) Tags(rows []wce.TagCount) error {
	s.tags += len(rows)
	return s.call("tags")
}

func campaignRows(n int) []wce.CampaignFunnel {
	rows := make([]wce.CampaignFunnel, n)
	for i := range rows {
		rows[i] = wce.CampaignFunnel{CampaignID: "c", Base: 1}
	}
	return rows
}

func newExportUseCase(reader *reportReaderStub) wc.ExportDispatchReportUseCase {
	return NewExportDispatchReportUseCase(reportCampaignFinderStub{campaign: ownedCampaign()}, reader, utc())
}

func longPeriod() wc.ReportPeriod {
	return wc.ReportPeriod{DateFrom: "2025-09-01", DateTo: "2026-10-05"}
}

func TestAnExportWalksEverySectionOfTheBlockOverAPeriodTheChartCannotShow(t *testing.T) {
	reader := &reportReaderStub{
		funnel:        wce.Funnel{Base: 502},
		reasons:       []wce.FailureReasonCount{{Code: 131026, Count: 2}},
		campaignPages: [][]wce.CampaignFunnel{campaignRows(wce.ExportPageRows), campaignRows(2)},
		tagPages:      [][]wce.TagCount{{{LabelID: "l1", Count: 3}}},
	}
	sink := &sinkRecorder{}

	if err := newExportUseCase(reader).Export(context.Background(), workspaceAccess(), longPeriod(), sink); err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	if want := []string{"summary", "daily", "campaigns", "campaigns", "failures", "tags"}; !reflect.DeepEqual(sink.calls, want) {
		t.Fatalf("sections = %v, want %v", sink.calls, want)
	}
	if want := []wce.Page{wce.FirstPage(wce.ExportPageRows), wce.FirstPage(wce.ExportPageRows).Next()}; !reflect.DeepEqual(reader.pagesAsked, want) {
		t.Fatalf("campaign pages = %+v, want %+v so a long period is never read whole", reader.pagesAsked, want)
	}
	if sink.campaigns != wce.ExportPageRows+2 || sink.tags != 1 || sink.summary.Funnel.Base != 502 || len(sink.failures.Reasons) != 1 {
		t.Fatalf("sink = %+v", sink)
	}
	if sink.daily.From != "2025-09-01" || sink.daily.To != "2026-10-05" || len(sink.daily.Days) != 400 {
		t.Fatalf("daily covers %s to %s in %d days, want the whole chosen period", sink.daily.From, sink.daily.To, len(sink.daily.Days))
	}
	for _, scope := range reader.scopes {
		if scope.WorkspaceID != "ws1" || !reflect.DeepEqual(scope.DepartmentIDs, []string{"d1"}) || scope.ExcludedType != string(wc.CampaignTypeOrganic) {
			t.Fatalf("scope = %+v, want the block's own scope", scope)
		}
	}
}

func TestAnExportRefusesAPeriodPastItsMaximumBeforeReading(t *testing.T) {
	reader := &reportReaderStub{}
	sink := &sinkRecorder{}

	err := newExportUseCase(reader).Export(context.Background(), workspaceAccess(), wc.ReportPeriod{DateFrom: "2023-01-01", DateTo: "2026-10-05"}, sink)

	if !errors.Is(err, wce.ErrReportWindowTooLong) || len(reader.reads) != 0 || len(sink.calls) != 0 {
		t.Fatalf("error = %v, reads %v, sections %v", err, reader.reads, sink.calls)
	}
}

func TestACampaignExportHasNoCampaignBreakdown(t *testing.T) {
	reader := &reportReaderStub{funnel: wce.Funnel{Base: 3}}
	sink := &sinkRecorder{}

	if err := newExportUseCase(reader).Export(context.Background(), campaignAccess(), longPeriod(), sink); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if want := []string{"summary", "daily", "failures", "tags"}; !reflect.DeepEqual(sink.calls, want) {
		t.Fatalf("sections = %v, want %v", sink.calls, want)
	}
	if sink.summary.CampaignName != "Lançamento" {
		t.Fatalf("summary = %+v, want the campaign it covers", sink.summary)
	}
}

func TestASheetThatRefusesStopsTheExportThere(t *testing.T) {
	reader := &reportReaderStub{funnel: wce.Funnel{}}
	sink := &sinkRecorder{failOn: "summary"}

	err := newExportUseCase(reader).Export(context.Background(), workspaceAccess(), longPeriod(), sink)

	if !errors.Is(err, errSinkStopped) || !reflect.DeepEqual(reader.reads, []string{"funnel"}) {
		t.Fatalf("error = %v, reads %v; want the refusal and nothing read after it", err, reader.reads)
	}
}

func TestACallerOutsideEveryDepartmentExportsNothing(t *testing.T) {
	reader := &reportReaderStub{}
	access := workspaceAccess()
	access.DepartmentsBlocked = true

	err := newExportUseCase(reader).Export(context.Background(), access, longPeriod(), &sinkRecorder{})

	if !errors.Is(err, wc.ErrCampaignAccessDenied) || len(reader.reads) != 0 {
		t.Fatalf("error = %v, reads %v", err, reader.reads)
	}
}

func TestACancelledExportStopsBetweenPages(t *testing.T) {
	reader := &reportReaderStub{
		funnel:        wce.Funnel{Base: 1},
		campaignPages: [][]wce.CampaignFunnel{campaignRows(wce.ExportPageRows), campaignRows(1)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &cancellingSink{sinkRecorder: &sinkRecorder{}, cancel: cancel}

	err := newExportUseCase(reader).Export(ctx, workspaceAccess(), longPeriod(), sink)

	if !errors.Is(err, context.Canceled) || len(reader.pagesAsked) != 1 {
		t.Fatalf("error = %v, pages %v", err, reader.pagesAsked)
	}
}

type cancellingSink struct {
	*sinkRecorder
	cancel context.CancelFunc
}

func (s *cancellingSink) Campaigns(rows []wce.CampaignFunnel) error {
	s.cancel()
	return s.sinkRecorder.Campaigns(rows)
}
