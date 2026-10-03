package advertising

import (
	"context"
	"time"

	ads "vozko/domain/advertising"
)

type SavedReportsUseCase struct {
	reports  ads.SavedReportRepository
	accounts ads.AccountRepository
	now      func() time.Time
}

func NewSavedReportsUseCase(reports ads.SavedReportRepository, accounts ads.AccountRepository) *SavedReportsUseCase {
	return &SavedReportsUseCase{reports: reports, accounts: accounts, now: time.Now}
}

type SavedReportInput struct {
	Name        string
	AdAccountID string
	Definition  ads.ReportDefinition
}

type ReportOptions struct {
	Templates       []ads.ReportTemplate
	Views           []ads.ReportView
	Levels          []ads.Level
	Breakdowns      []ads.Breakdown
	Metrics         []ads.ReportMetric
	TrendMetrics    []ads.ReportMetric
	BreakdownGroups [][]ads.Breakdown
}

func (uc *SavedReportsUseCase) Options() ReportOptions {
	return ReportOptions{
		Templates:       ads.ReportTemplates(),
		Views:           ads.ReportViews(),
		Levels:          []ads.Level{ads.LevelCampaign, ads.LevelAdSet, ads.LevelAd},
		Breakdowns:      ads.ReportBreakdowns(),
		Metrics:         ads.ReportMetrics(),
		TrendMetrics:    ads.TrendMetrics(),
		BreakdownGroups: ads.BreakdownGroups(),
	}
}

func (uc *SavedReportsUseCase) List(ctx context.Context, workspaceID string) ([]*ads.SavedReport, error) {
	return uc.reports.ListByWorkspace(ctx, workspaceID)
}

func (uc *SavedReportsUseCase) Open(ctx context.Context, workspaceID, id string) (*ads.SavedReport, error) {
	report, err := uc.reports.Find(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	at := uc.now()
	if err := uc.reports.MarkOpened(ctx, workspaceID, id, at); err != nil {
		return nil, err
	}
	report.LastOpenedAt = &at
	return report, nil
}

func (uc *SavedReportsUseCase) Create(ctx context.Context, workspaceID, userID string, in SavedReportInput) (*ads.SavedReport, error) {
	report := &ads.SavedReport{WorkspaceID: workspaceID, CreatedBy: userID}
	if err := uc.apply(ctx, report, in); err != nil {
		return nil, err
	}
	at := uc.now()
	report.LastOpenedAt = &at
	if err := uc.reports.Create(ctx, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (uc *SavedReportsUseCase) Update(ctx context.Context, workspaceID, id string, in SavedReportInput) (*ads.SavedReport, error) {
	report, err := uc.reports.Find(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := uc.apply(ctx, report, in); err != nil {
		return nil, err
	}
	if err := uc.reports.Save(ctx, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (uc *SavedReportsUseCase) Delete(ctx context.Context, workspaceID, id string) error {
	return uc.reports.Delete(ctx, workspaceID, id)
}

func (uc *SavedReportsUseCase) apply(ctx context.Context, report *ads.SavedReport, in SavedReportInput) error {
	account, err := uc.accounts.FindByID(ctx, report.WorkspaceID, in.AdAccountID)
	if err != nil {
		return err
	}
	return report.Set(account.ID, in.Name, in.Definition)
}
