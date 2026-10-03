package advertising

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrReportNotFound = errors.New("saved ad report not found")

type ReportView string

const (
	ViewPivot ReportView = "pivot"
	ViewTrend ReportView = "trend"
	ViewBars  ReportView = "bars"
)

type ReportMetric string

const (
	ReportSpend               ReportMetric = "spend"
	ReportImpressions         ReportMetric = "impressions"
	ReportReach               ReportMetric = "reach"
	ReportFrequency           ReportMetric = "frequency"
	ReportClicks              ReportMetric = "clicks"
	ReportLinkClicks          ReportMetric = "linkClicks"
	ReportCTR                 ReportMetric = "ctr"
	ReportCPC                 ReportMetric = "cpc"
	ReportCPM                 ReportMetric = "cpm"
	ReportResults             ReportMetric = "results"
	ReportCostPerResult       ReportMetric = "costPerResult"
	ReportConversations       ReportMetric = "conversations"
	ReportCostPerConversation ReportMetric = "costPerConversation"
	ReportThruPlays           ReportMetric = "thruPlays"
	ReportCostPerThruPlay     ReportMetric = "costPerThruPlay"
)

var reportMetrics = []ReportMetric{
	ReportSpend, ReportImpressions, ReportReach, ReportFrequency, ReportClicks, ReportLinkClicks, ReportCTR, ReportCPC, ReportCPM,
	ReportResults, ReportCostPerResult, ReportConversations, ReportCostPerConversation, ReportThruPlays, ReportCostPerThruPlay,
}

var trendMetrics = []ReportMetric{ReportSpend, ReportImpressions, ReportLinkClicks, ReportResults, ReportConversations}

type ReportPreset string

const PresetCustom ReportPreset = "custom"

var reportPresets = []ReportPreset{
	"today", "yesterday", "todayAndYesterday", "last7", "last14", "last28", "last30",
	"thisWeek", "lastWeek", "thisMonth", "lastMonth", "maximum", PresetCustom,
}

type MetricKind string

const (
	KindMoney   MetricKind = "money"
	KindCount   MetricKind = "count"
	KindPercent MetricKind = "percent"
	KindDecimal MetricKind = "decimal"
)

func (m ReportMetric) Kind() MetricKind {
	switch m {
	case ReportSpend, ReportCPC, ReportCPM, ReportCostPerResult, ReportCostPerConversation, ReportCostPerThruPlay:
		return KindMoney
	case ReportCTR:
		return KindPercent
	case ReportFrequency:
		return KindDecimal
	}
	return KindCount
}

func KindsOf(metrics []ReportMetric) map[ReportMetric]MetricKind {
	out := make(map[ReportMetric]MetricKind, len(metrics))
	for _, m := range metrics {
		out[m] = m.Kind()
	}
	return out
}

func ReportViews() []ReportView { return []ReportView{ViewPivot, ViewTrend, ViewBars} }

func ReportMetrics() []ReportMetric { return slices.Clone(reportMetrics) }

func TrendMetrics() []ReportMetric { return slices.Clone(trendMetrics) }

func ReportBreakdowns() []Breakdown {
	var out []Breakdown
	for _, group := range breakdownGroups {
		for _, b := range group {
			if !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}
	return out
}

type ReportDefinition struct {
	View       ReportView     `json:"view"`
	Level      Level          `json:"level"`
	Breakdowns []Breakdown    `json:"breakdowns"`
	Metrics    []ReportMetric `json:"metrics"`
	DatePreset ReportPreset   `json:"datePreset"`
	Since      string         `json:"since,omitempty"`
	Until      string         `json:"until,omitempty"`
}

func (d ReportDefinition) Validate() error {
	v := newIssues()
	if !slices.Contains(ReportViews(), d.View) {
		v.add("definition.view", "invalid")
	}
	if !d.Level.Valid() {
		v.add("definition.level", "invalid")
	}
	if d.View == ViewTrend && len(d.Breakdowns) > 0 {
		v.add("definition.breakdowns", "not_for_trend")
	} else if ValidateBreakdowns(d.Breakdowns) != nil {
		v.add("definition.breakdowns", "invalid_combination")
	}
	allowed := reportMetrics
	if d.View == ViewTrend {
		allowed = trendMetrics
	}
	if len(d.Metrics) == 0 {
		v.add("definition.metrics", "required")
	}
	for i, m := range d.Metrics {
		if !slices.Contains(allowed, m) || slices.Index(d.Metrics, m) != i {
			v.add("definition.metrics", "invalid")
			break
		}
	}
	d.validateDates(v)
	return v.err()
}

func (d ReportDefinition) validateDates(v issues) {
	if !slices.Contains(reportPresets, d.DatePreset) {
		v.add("definition.datePreset", "invalid")
		return
	}
	if d.DatePreset != PresetCustom {
		if d.Since != "" || d.Until != "" {
			v.add("definition.since", "only_for_custom")
		}
		return
	}
	if _, err := NewDateRange(d.Since, d.Until); err != nil {
		v.add("definition.since", "invalid_range")
	}
}

type ReportTemplate struct {
	Key        string
	Definition ReportDefinition
}

func ReportTemplates() []ReportTemplate {
	pivot := func(level Level, breakdowns []Breakdown, metrics ...ReportMetric) ReportDefinition {
		return ReportDefinition{View: ViewPivot, Level: level, Breakdowns: breakdowns, Metrics: metrics, DatePreset: "last30"}
	}
	return []ReportTemplate{
		{"roi_snapshot", pivot(LevelCampaign, nil, ReportSpend, ReportResults, ReportCostPerResult, ReportConversations, ReportCostPerConversation)},
		{"reach_frequency", pivot(LevelCampaign, []Breakdown{BreakdownPlatform, BreakdownPosition}, ReportReach, ReportFrequency, ReportImpressions, ReportSpend)},
		{"overall_performance", pivot(LevelCampaign, nil, ReportSpend, ReportImpressions, ReportReach, ReportClicks, ReportCTR, ReportCPC, ReportCPM, ReportResults, ReportCostPerResult)},
		{"signups_summary", pivot(LevelAd, nil, ReportResults, ReportCostPerResult, ReportSpend)},
		{"age_gender", pivot(LevelCampaign, []Breakdown{BreakdownAge, BreakdownGender}, ReportImpressions, ReportReach, ReportResults, ReportSpend)},
		{"engagement", pivot(LevelAd, nil, ReportImpressions, ReportClicks, ReportCTR, ReportLinkClicks)},
	}
}

type SavedReport struct {
	ID           string
	WorkspaceID  string
	AdAccountID  string
	Name         string
	Definition   ReportDefinition
	CreatedBy    string
	LastOpenedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (r *SavedReport) Set(accountID, name string, definition ReportDefinition) error {
	name = strings.TrimSpace(name)
	v := newIssues()
	v.text("name", name, true, maxNameRunes)
	if err := v.err(); err != nil {
		return err
	}
	if err := definition.Validate(); err != nil {
		return err
	}
	r.AdAccountID, r.Name, r.Definition = accountID, name, definition
	return nil
}

type SavedReportRepository interface {
	Create(ctx context.Context, r *SavedReport) error
	Find(ctx context.Context, workspaceID, id string) (*SavedReport, error)
	Save(ctx context.Context, r *SavedReport) error
	MarkOpened(ctx context.Context, workspaceID, id string, at time.Time) error
	Delete(ctx context.Context, workspaceID, id string) error
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*SavedReport, error)
}
