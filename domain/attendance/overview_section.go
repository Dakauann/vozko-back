package attendance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

type Section string

const (
	SectionSummary Section = "summary"
	SectionTrend   Section = "trend"
	SectionStages  Section = "stages"
	SectionBacklog Section = "backlog"
	SectionTeam    Section = "team"
	SectionRework  Section = "rework"
)

const fingerprintVersion = "v1"

func Sections() []Section {
	return []Section{SectionSummary, SectionTrend, SectionStages, SectionBacklog, SectionTeam, SectionRework}
}

func ParseSection(raw string) (Section, bool) {
	for _, section := range Sections() {
		if string(section) == raw {
			return section, true
		}
	}
	return "", false
}

type SummarySection struct {
	Filter             OverviewFilter           `json:"filter"`
	KPIs               OverviewKPIs             `json:"kpis"`
	Hourly             []HourlyPoint            `json:"hourly"`
	StatusDistribution StatusDistribution       `json:"status_distribution"`
	FRT                OverviewFRT              `json:"frt"`
	AI                 OverviewAI               `json:"ai"`
	ChannelMix         []ChannelSlice           `json:"channel_mix"`
	Messaging          OverviewMessaging        `json:"messaging"`
	Reopen             OverviewReopen           `json:"reopen"`
	FinishedBySource   OverviewFinishedBySource `json:"finished_by_source"`
	Quality            Quality                  `json:"quality"`
	Revenue            Revenue                  `json:"revenue"`
	Period             Period                   `json:"period"`
	Projections        []MetricProjection       `json:"projections"`
	Standing           Standing                 `json:"standing"`
	GeneratedAt        time.Time                `json:"generated_at"`
	Definitions        MetricDefinitions        `json:"definitions"`
}

type TeamSection struct {
	ByDepartment []DepartmentRow `json:"by_department"`
	ByMember     []MemberRow     `json:"by_member"`
	TeamRanking  TeamRanking     `json:"team_ranking"`
}

type TrendSection struct {
	Trend Trend `json:"trend"`
}

type StagesSection struct {
	Stages OverviewStages `json:"stages"`
}

type BacklogSection struct {
	BacklogXray BacklogXray `json:"backlog_xray"`
}

type ReworkSection struct {
	Rework OverviewRework `json:"rework"`
}

type LiveSection struct {
	Queue     OverviewQueue     `json:"queue"`
	Occupancy OverviewOccupancy `json:"occupancy"`
	Live      OverviewLive      `json:"live"`
}

func (f OverviewFilter) Fingerprint(section Section) string {
	parts := []string{
		fingerprintVersion,
		string(section),
		fingerprintTime(f.DateFrom),
		fingerprintTime(f.DateTo),
		f.DepartmentID,
		f.MemberID,
		f.CampaignID,
		f.CampaignType,
		f.Channel,
	}
	switch section {
	case SectionTeam:
		parts = append(parts, f.RankMetric, strconv.FormatBool(f.IncludeAI))
	case SectionTrend:
		parts = append(parts, strconv.Itoa(f.TrendBuckets))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func fingerprintTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return strconv.FormatInt(t.UnixNano(), 10)
}

type OverviewSectionsUseCase interface {
	Summary(ctx context.Context, workspaceID string, filter OverviewFilter) (*SummarySection, error)
	Trend(ctx context.Context, workspaceID string, filter OverviewFilter) (*TrendSection, error)
	Stages(ctx context.Context, workspaceID string, filter OverviewFilter) (*StagesSection, error)
	Backlog(ctx context.Context, workspaceID string, filter OverviewFilter) (*BacklogSection, error)
	Team(ctx context.Context, workspaceID string, filter OverviewFilter) (*TeamSection, error)
	Rework(ctx context.Context, workspaceID string, filter OverviewFilter) (*ReworkSection, error)
}
