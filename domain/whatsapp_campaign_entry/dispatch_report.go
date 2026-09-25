package whatsapp_campaign_entry

import (
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/campaign"
)

const (
	MaxReportDays  = 92
	ReportTagLimit = 10
	reportDayForm  = "2006-01-02"
)

var (
	ErrReportWindowInvalid = errors.New("dispatch report window is invalid")
	ErrReportWindowTooLong = errors.New("dispatch report window exceeds the maximum number of days")
)

type DayWindow struct {
	From     time.Time
	To       time.Time
	Location *time.Location
}

func NewDayWindow(fromDay, toDay string, loc *time.Location) (DayWindow, error) {
	if loc == nil {
		return DayWindow{}, ErrReportWindowInvalid
	}
	from, err := time.ParseInLocation(reportDayForm, fromDay, loc)
	if err != nil {
		return DayWindow{}, ErrReportWindowInvalid
	}
	last, err := time.ParseInLocation(reportDayForm, toDay, loc)
	if err != nil || last.Before(from) {
		return DayWindow{}, ErrReportWindowInvalid
	}
	w := DayWindow{From: from, To: last.AddDate(0, 0, 1), Location: loc}
	if w.To.After(from.AddDate(0, 0, MaxReportDays)) {
		return DayWindow{}, ErrReportWindowTooLong
	}
	return w, nil
}

func (w DayWindow) FirstDay() string {
	return w.From.Format(reportDayForm)
}

func (w DayWindow) LastDay() string {
	return w.To.AddDate(0, 0, -1).Format(reportDayForm)
}

func (w DayWindow) Fingerprint() string {
	return w.FirstDay() + ":" + w.LastDay() + ":" + w.Location.String()
}

func (w DayWindow) Days() []string {
	var days []string
	for d := w.From; d.Before(w.To); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(reportDayForm))
	}
	return days
}

type Funnel struct {
	Base             int64      `json:"base"`
	Sent             int64      `json:"sent"`
	Delivered        int64      `json:"delivered"`
	Read             int64      `json:"read"`
	Replied          int64      `json:"replied"`
	Failed           int64      `json:"failed"`
	AwaitingDelivery int64      `json:"awaitingDelivery"`
	Pending          int64      `json:"pending"`
	NotEligible      int64      `json:"notEligible"`
	TrackedSince     *time.Time `json:"trackedSince"`
}

type DayCount struct {
	Day       string `json:"day"`
	Sent      int64  `json:"sent"`
	Delivered int64  `json:"delivered"`
	Read      int64  `json:"read"`
	Replied   int64  `json:"replied"`
}

type FailureReasonCount struct {
	Code  int   `json:"code"`
	Count int64 `json:"count"`
}

type TagCount struct {
	LabelID string `json:"labelId"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Count   int64  `json:"count"`
}

type DispatchReportReader interface {
	Funnel(scope ReportScope) (Funnel, error)
	Daily(scope ReportScope, window DayWindow) ([]DayCount, error)
	FailureReasons(scope ReportScope) ([]FailureReasonCount, error)
	Tags(scope ReportScope, limit int) ([]TagCount, error)
	Campaigns(scope ReportScope, limit int) ([]CampaignFunnel, error)
}

func DenseDays(window DayWindow, sparse []DayCount) []DayCount {
	byDay := make(map[string]DayCount, len(sparse))
	for _, d := range sparse {
		byDay[d.Day] = d
	}
	days := window.Days()
	out := make([]DayCount, 0, len(days))
	for _, day := range days {
		count, ok := byDay[day]
		if !ok {
			count = DayCount{Day: day}
		}
		out = append(out, count)
	}
	return out
}

func StatusesProving(m campaign.Milestone) []SendStatus {
	var out []SendStatus
	for _, s := range AllStatuses() {
		for _, proven := range s.Milestones() {
			if proven == m {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

var ErrReportScopeInvalid = errors.New("dispatch report scope is invalid")

const ReportCampaignLimit = 50

type ReportScope struct {
	CampaignID    string
	WorkspaceID   string
	DepartmentIDs []string
	ExcludedType  string
	Window        DayWindow
}

func CampaignScope(campaignID string) (ReportScope, error) {
	if campaignID == "" {
		return ReportScope{}, ErrReportScopeInvalid
	}
	return ReportScope{CampaignID: campaignID}, nil
}

func WorkspaceScope(workspaceID string, departmentIDs []string, excludedType string, window DayWindow) (ReportScope, error) {
	if workspaceID == "" || window.Location == nil {
		return ReportScope{}, ErrReportScopeInvalid
	}
	departments := append([]string(nil), departmentIDs...)
	sort.Strings(departments)
	return ReportScope{
		WorkspaceID:   workspaceID,
		DepartmentIDs: departments,
		ExcludedType:  excludedType,
		Window:        window,
	}, nil
}

func (s ReportScope) IsCampaign() bool {
	return s.CampaignID != ""
}

func (s ReportScope) Fingerprint() string {
	if s.IsCampaign() {
		return "campaign:" + s.CampaignID
	}
	return "workspace:" + s.WorkspaceID + ":" + strings.Join(s.DepartmentIDs, ",") + ":" + s.Window.Fingerprint()
}

type CampaignFunnel struct {
	CampaignID   string `json:"campaignId"`
	CampaignName string `json:"campaignName"`
	Base         int64  `json:"base"`
	Sent         int64  `json:"sent"`
	Delivered    int64  `json:"delivered"`
	Read         int64  `json:"read"`
	Replied      int64  `json:"replied"`
	Failed       int64  `json:"failed"`
}
