package whatsapp_campaign

import (
	"context"
	"errors"
	"time"

	"vozko/domain/whatsapp_campaign_entry"
)

var ErrCampaignAccessDenied = errors.New("whatsapp campaign access denied")

type ReportSection string

const (
	ReportSectionSummary   ReportSection = "summary"
	ReportSectionDaily     ReportSection = "daily"
	ReportSectionFailures  ReportSection = "failures"
	ReportSectionTags      ReportSection = "tags"
	ReportSectionCampaigns ReportSection = "campaigns"
)

func ReportSections() []ReportSection {
	return []ReportSection{ReportSectionSummary, ReportSectionDaily, ReportSectionFailures, ReportSectionTags, ReportSectionCampaigns}
}

func ParseReportSection(raw string) (ReportSection, bool) {
	for _, section := range ReportSections() {
		if string(section) == raw {
			return section, true
		}
	}
	return "", false
}

type ReportPeriod struct {
	DateFrom string
	DateTo   string
}

type ReportLocator interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

type ReportAccess struct {
	WorkspaceID        string
	CampaignID         string
	DepartmentIDs      []string
	DepartmentsBlocked bool
	AllowDepartment    func(departmentID string) bool
}

type ReportSummary struct {
	CampaignID        string                         `json:"campaignId"`
	CampaignName      string                         `json:"campaignName"`
	TemplateName      string                         `json:"templateName"`
	CampaignCreatedAt *time.Time                     `json:"campaignCreatedAt"`
	Funnel            whatsapp_campaign_entry.Funnel `json:"funnel"`
}

type ReportDaily struct {
	From     string                             `json:"from"`
	To       string                             `json:"to"`
	Timezone string                             `json:"timezone"`
	Days     []whatsapp_campaign_entry.DayCount `json:"days"`
}

type ReportFailures struct {
	Reasons []whatsapp_campaign_entry.FailureReasonCount `json:"reasons"`
}

type ReportTags struct {
	Tags []whatsapp_campaign_entry.TagCount `json:"tags"`
}

type ReportCampaigns struct {
	Campaigns []whatsapp_campaign_entry.CampaignFunnel `json:"campaigns"`
}

type GetDispatchReportUseCase interface {
	Summary(ctx context.Context, access ReportAccess, period ReportPeriod) (*ReportSummary, error)
	Daily(ctx context.Context, access ReportAccess, period ReportPeriod) (*ReportDaily, error)
	Failures(ctx context.Context, access ReportAccess, period ReportPeriod) (*ReportFailures, error)
	Tags(ctx context.Context, access ReportAccess, period ReportPeriod) (*ReportTags, error)
	Campaigns(ctx context.Context, access ReportAccess, period ReportPeriod) (*ReportCampaigns, error)
}
