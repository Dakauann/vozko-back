package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/cache"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

const reportCachePrefix = "whatsapp:dispatch-report"

var errReportZoneUnresolved = errors.New("dispatch report: the workspace timezone could not be resolved")

type campaignFinder interface {
	FindByID(campaignID string) (*wc.Campaign, error)
}

type ReportCaching struct {
	Memo cache.Memo
	Gate cache.Gate
	TTL  time.Duration
}

type getDispatchReportUseCase struct {
	campaigns campaignFinder
	reader    wce.DispatchReportReader
	locator   wc.ReportLocator
	caching   ReportCaching
}

type reportTarget struct {
	scope    wce.ReportScope
	campaign *wc.Campaign
	window   wce.DayWindow
}

func NewGetDispatchReportUseCase(
	campaigns campaignFinder,
	reader wce.DispatchReportReader,
	locator wc.ReportLocator,
	caching ReportCaching,
) wc.GetDispatchReportUseCase {
	return &getDispatchReportUseCase{campaigns: campaigns, reader: reader, locator: locator, caching: caching}
}

func (uc *getDispatchReportUseCase) Summary(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportSummary, error) {
	target, err := uc.resolve(ctx, access, period)
	if err != nil {
		return nil, err
	}
	return rememberReport(ctx, uc, target.scope, wc.ReportSectionSummary, "", func() (*wc.ReportSummary, error) {
		funnel, err := uc.reader.Funnel(target.scope)
		if err != nil {
			return nil, err
		}
		out := &wc.ReportSummary{Funnel: funnel}
		if c := target.campaign; c != nil {
			created := c.CreatedAt
			out.CampaignID = c.ID
			out.CampaignName = c.Name
			out.TemplateName = c.TemplateName
			out.CampaignCreatedAt = &created
		}
		return out, nil
	})
}

func (uc *getDispatchReportUseCase) Daily(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportDaily, error) {
	target, err := uc.resolve(ctx, access, period)
	if err != nil {
		return nil, err
	}
	window := target.window
	return rememberReport(ctx, uc, target.scope, wc.ReportSectionDaily, window.Fingerprint(), func() (*wc.ReportDaily, error) {
		days, err := uc.reader.Daily(target.scope, window)
		if err != nil {
			return nil, err
		}
		return &wc.ReportDaily{
			From:     window.FirstDay(),
			To:       window.LastDay(),
			Timezone: window.Location.String(),
			Days:     wce.DenseDays(window, days),
		}, nil
	})
}

func (uc *getDispatchReportUseCase) Failures(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportFailures, error) {
	target, err := uc.resolve(ctx, access, period)
	if err != nil {
		return nil, err
	}
	return rememberReport(ctx, uc, target.scope, wc.ReportSectionFailures, "", func() (*wc.ReportFailures, error) {
		reasons, err := uc.reader.FailureReasons(target.scope)
		if err != nil {
			return nil, err
		}
		return &wc.ReportFailures{Reasons: nonNil(reasons)}, nil
	})
}

func (uc *getDispatchReportUseCase) Tags(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportTags, error) {
	target, err := uc.resolve(ctx, access, period)
	if err != nil {
		return nil, err
	}
	return rememberReport(ctx, uc, target.scope, wc.ReportSectionTags, "", func() (*wc.ReportTags, error) {
		tags, err := uc.reader.Tags(target.scope, wce.ReportTagLimit)
		if err != nil {
			return nil, err
		}
		return &wc.ReportTags{Tags: nonNil(tags)}, nil
	})
}

func (uc *getDispatchReportUseCase) Campaigns(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (*wc.ReportCampaigns, error) {
	target, err := uc.resolve(ctx, access, period)
	if err != nil {
		return nil, err
	}
	if target.scope.IsCampaign() {
		return nil, wce.ErrReportScopeInvalid
	}
	return rememberReport(ctx, uc, target.scope, wc.ReportSectionCampaigns, "", func() (*wc.ReportCampaigns, error) {
		rows, err := uc.reader.Campaigns(target.scope, wce.ReportCampaignLimit)
		if err != nil {
			return nil, err
		}
		return &wc.ReportCampaigns{Campaigns: nonNil(rows)}, nil
	})
}

func (uc *getDispatchReportUseCase) resolve(ctx context.Context, access wc.ReportAccess, period wc.ReportPeriod) (reportTarget, error) {
	if access.WorkspaceID == "" || access.AllowDepartment == nil {
		return reportTarget{}, wc.ErrCampaignAccessDenied
	}
	if access.CampaignID == "" {
		if access.DepartmentsBlocked {
			return reportTarget{}, wc.ErrCampaignAccessDenied
		}
		window, err := uc.window(ctx, access.WorkspaceID, singleDepartment(access.DepartmentIDs), period)
		if err != nil {
			return reportTarget{}, err
		}
		scope, err := wce.WorkspaceScope(access.WorkspaceID, access.DepartmentIDs, string(wc.CampaignTypeOrganic), window)
		return reportTarget{scope: scope, window: window}, err
	}

	campaign, err := uc.campaigns.FindByID(access.CampaignID)
	if err != nil {
		return reportTarget{}, err
	}
	if campaign.WorkspaceID != access.WorkspaceID || !access.AllowDepartment(campaign.DepartmentID) {
		return reportTarget{}, wc.ErrCampaignAccessDenied
	}
	window, err := uc.window(ctx, access.WorkspaceID, campaign.DepartmentID, period)
	if err != nil {
		return reportTarget{}, err
	}
	scope, err := wce.CampaignScope(campaign.ID)
	return reportTarget{scope: scope, campaign: campaign, window: window}, err
}

func (uc *getDispatchReportUseCase) window(ctx context.Context, workspaceID, departmentID string, period wc.ReportPeriod) (wce.DayWindow, error) {
	if uc.locator == nil {
		return wce.DayWindow{}, errReportZoneUnresolved
	}
	loc, err := uc.locator.Location(ctx, workspaceID, departmentID)
	if err != nil {
		return wce.DayWindow{}, err
	}
	if loc == nil {
		return wce.DayWindow{}, errReportZoneUnresolved
	}
	return wce.NewDayWindow(period.DateFrom, period.DateTo, loc)
}

func singleDepartment(departmentIDs []string) string {
	if len(departmentIDs) == 1 {
		return departmentIDs[0]
	}
	return ""
}

func rememberReport[T any](
	ctx context.Context,
	uc *getDispatchReportUseCase,
	scope wce.ReportScope,
	section wc.ReportSection,
	variant string,
	compute func() (T, error),
) (T, error) {
	key := reportCachePrefix + ":" + scope.Fingerprint() + ":" + string(section) + ":" + variant
	return cache.Remember(ctx, uc.caching.Memo, key, uc.caching.TTL, func(ctx context.Context) (T, error) {
		var out T
		err := cache.Gated(ctx, uc.caching.Gate, func(context.Context) error {
			var err error
			out, err = compute()
			return err
		})
		return out, err
	})
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
