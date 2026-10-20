package whatsapp_campaign_usecase

import (
	"context"

	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type exportDispatchReportUseCase struct {
	targets dispatchTargets
	reader  wce.DispatchReportReader
}

func NewExportDispatchReportUseCase(
	campaigns campaignFinder,
	reader wce.DispatchReportReader,
	locator wc.ReportLocator,
) wc.ExportDispatchReportUseCase {
	return &exportDispatchReportUseCase{targets: dispatchTargets{campaigns: campaigns, locator: locator}, reader: reader}
}

func (uc *exportDispatchReportUseCase) Export(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod, sink wc.DispatchExportSink) error {
	target, err := uc.targets.resolve(ctx, access, period, wce.MaxExportDays)
	if err != nil {
		return err
	}
	steps := []func(context.Context, reportTarget, wc.DispatchExportSink) error{
		uc.exportSummary,
		uc.exportDaily,
		uc.exportCampaigns,
		uc.exportFailures,
		uc.exportTags,
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step(ctx, target, sink); err != nil {
			return err
		}
	}
	return nil
}

func (uc *exportDispatchReportUseCase) exportSummary(_ context.Context, target reportTarget, sink wc.DispatchExportSink) error {
	funnel, err := uc.reader.Funnel(target.scope)
	if err != nil {
		return err
	}
	return sink.Summary(summaryOf(funnel, target.campaign))
}

func (uc *exportDispatchReportUseCase) exportDaily(_ context.Context, target reportTarget, sink wc.DispatchExportSink) error {
	days, err := uc.reader.Daily(target.scope, target.window)
	if err != nil {
		return err
	}
	return sink.Daily(dailyOf(target.window, days))
}

func (uc *exportDispatchReportUseCase) exportCampaigns(ctx context.Context, target reportTarget, sink wc.DispatchExportSink) error {
	if target.scope.IsCampaign() {
		return nil
	}
	return eachPage(ctx, func(page wce.Page) ([]wce.CampaignFunnel, error) {
		return uc.reader.Campaigns(target.scope, page)
	}, sink.Campaigns)
}

func (uc *exportDispatchReportUseCase) exportFailures(_ context.Context, target reportTarget, sink wc.DispatchExportSink) error {
	reasons, err := uc.reader.FailureReasons(target.scope)
	if err != nil {
		return err
	}
	return sink.Failures(&wc.ReportFailures{Reasons: nonNil(reasons)})
}

func (uc *exportDispatchReportUseCase) exportTags(ctx context.Context, target reportTarget, sink wc.DispatchExportSink) error {
	return eachPage(ctx, func(page wce.Page) ([]wce.TagCount, error) {
		return uc.reader.Tags(target.scope, page)
	}, sink.Tags)
}

func eachPage[T any](ctx context.Context, read func(wce.Page) ([]T, error), write func([]T) error) error {
	for page := wce.FirstPage(wce.ExportPageRows); ; page = page.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := read(page)
		if err != nil {
			return err
		}
		if len(rows) > 0 || page.Offset == 0 {
			if err := write(rows); err != nil {
				return err
			}
		}
		if page.IsLast(len(rows)) {
			return nil
		}
	}
}
