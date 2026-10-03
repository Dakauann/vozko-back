package advertising

import (
	"context"
	"log"

	ads "vozko/domain/advertising"
	"vozko/domain/report"
)

type reportLive interface {
	Insights(ctx context.Context, q ads.LiveQuery) (*LiveReport, error)
}

type reportTrend interface {
	Trend(ctx context.Context, q ReportQuery) (*Trend, error)
}

type ReportRunsUseCase struct {
	live    reportLive
	trend   reportTrend
	objects ads.ObjectRepository
	reports ads.SavedReportRepository
	exports ads.ReportExportRepository
}

func NewReportRunsUseCase(live reportLive, trend reportTrend, objects ads.ObjectRepository, reports ads.SavedReportRepository, exports ads.ReportExportRepository) *ReportRunsUseCase {
	return &ReportRunsUseCase{live: live, trend: trend, objects: objects, reports: reports, exports: exports}
}

type ReportRunInput struct {
	WorkspaceID string
	AccountID   string
	Definition  ads.ReportDefinition
	Range       ads.DateRange
	ObjectIDs   []string
	Windows     []ads.AttributionWindow
}

type ReportRun struct {
	Currency string
	Table    ads.ReportTable
	Series   []ads.ReportDay
}

func (r *ReportRun) Rows() int {
	if r.Table.View == ads.ViewTrend {
		return len(r.Series)
	}
	return len(r.Table.Rows)
}

func (r *ReportRun) CSV(labels ads.ExportLabels) []byte {
	section := r.Table.CSVSection(labels)
	if r.Table.View == ads.ViewTrend {
		section = ads.SeriesCSVSection(r.Table.Metrics, r.Series, labels)
	}
	return []byte(report.BuildCSVDocument([]report.CSVSection{section}, true))
}

func (uc *ReportRunsUseCase) Run(ctx context.Context, in ReportRunInput) (*ReportRun, error) {
	def := in.Definition
	if err := def.Validate(); err != nil {
		return nil, err
	}
	if err := in.Range.Validate(); err != nil {
		return nil, err
	}
	if def.View == ads.ViewTrend {
		return uc.runTrend(ctx, in)
	}
	live, err := uc.live.Insights(ctx, ads.LiveQuery{
		WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, Level: def.Level, ObjectIDs: in.ObjectIDs,
		Range: in.Range, Breakdowns: def.Breakdowns, Windows: in.Windows,
	})
	if err != nil {
		return nil, err
	}
	objects, err := uc.objects.List(ctx, ads.ObjectQuery{WorkspaceID: in.WorkspaceID, AdAccountID: live.Account.ID, Level: def.Level, IncludeRemoved: true})
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(objects))
	for _, o := range objects {
		names[o.MetaID] = o.Name
	}
	table, err := ads.BuildReportTable(def, live.Rows, names)
	if err != nil {
		return nil, err
	}
	return &ReportRun{Currency: live.Account.Currency, Table: table}, nil
}

func (uc *ReportRunsUseCase) runTrend(ctx context.Context, in ReportRunInput) (*ReportRun, error) {
	trend, err := uc.trend.Trend(ctx, ReportQuery{WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, Range: in.Range})
	if err != nil {
		return nil, err
	}
	days := make([]ads.DayMetrics, 0, len(trend.Points))
	for _, p := range trend.Points {
		days = append(days, ads.DayMetrics{Day: p.Day, Metrics: p.Metrics})
	}
	def := in.Definition
	return &ReportRun{
		Currency: trend.Account.Currency,
		Table:    ads.ReportTable{View: ads.ViewTrend, Breakdowns: []ads.Breakdown{}, Metrics: def.Metrics, Rows: []ads.ReportTableRow{}, Totals: ads.ReportCells{}},
		Series:   ads.BuildReportSeries(def.Metrics, days),
	}, nil
}

type ReportExportInput struct {
	ReportRunInput
	UserID   string
	Name     string
	ReportID string
	Labels   ads.ExportLabels
}

func (uc *ReportRunsUseCase) Export(ctx context.Context, in ReportExportInput) (*ads.ReportExport, error) {
	if in.ReportID != "" {
		saved, err := uc.reports.Find(ctx, in.WorkspaceID, in.ReportID)
		if err != nil {
			return nil, err
		}
		if saved.AdAccountID != in.AccountID {
			return nil, ads.ErrReportNotFound
		}
	}
	run, err := uc.Run(ctx, in.ReportRunInput)
	if err != nil {
		return nil, err
	}
	export, err := ads.NewReportExport(in.WorkspaceID, in.AccountID, in.ReportID, in.UserID, in.Name, in.Range, run.Rows(), run.CSV(in.Labels))
	if err != nil {
		return nil, err
	}
	if err := uc.exports.Create(ctx, export); err != nil {
		return nil, err
	}
	if err := uc.exports.KeepNewest(ctx, in.WorkspaceID, ads.KeptReportExports); err != nil {
		log.Printf("[ads] could not trim the report exports of workspace %s: %v", in.WorkspaceID, err)
	}
	return export, nil
}

func (uc *ReportRunsUseCase) Exports(ctx context.Context, workspaceID string) ([]*ads.ReportExport, error) {
	return uc.exports.List(ctx, workspaceID, ads.KeptReportExports)
}

func (uc *ReportRunsUseCase) ExportFile(ctx context.Context, workspaceID, id string) (*ads.ReportExport, error) {
	return uc.exports.Find(ctx, workspaceID, id)
}

func (uc *ReportRunsUseCase) DeleteExport(ctx context.Context, workspaceID, id string) error {
	return uc.exports.Delete(ctx, workspaceID, id)
}
