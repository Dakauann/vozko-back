package attendance

import (
	"context"
	"time"
)

type QualityPolicy struct {
	Enabled      bool
	EnabledAt    *time.Time
	Threshold    float64
	DurableCodes []string
}

func (p QualityPolicy) Measurable() bool {
	return p.Enabled && p.EnabledAt != nil
}

type TrendBucketRow struct {
	Bucket   string
	Engaged  int64
	Finished int64
	Created  int64
	Pending  int64
}

type TrendResult struct {
	Buckets    []TrendBucketRow
	Unbucketed int64
}

type RevenueMonthRow struct {
	Bucket     string
	Currency   string
	ValueCents int64
	WonCount   int64
}

type Repository interface {
	GetAttendantStats(workspaceID string, filter StatsFilter) ([]AttendantStats, error)

	GetWindowStats(workspaceID string, filter StatsFilter) (*WindowStats, error)

	GetResponseTimeDistribution(workspaceID string, filter StatsFilter) (*ResponseTimeDistribution, error)

	GetAIAgentStats(workspaceID string, filter StatsFilter) ([]AIAgentStats, error)

	GetFRTStats(workspaceID string, filter StatsFilter) (*FRTStats, error)

	ReadSummary(ctx context.Context, workspaceID string, filter OverviewFilter) (*SummarySection, error)

	ReadTeam(ctx context.Context, workspaceID string, filter OverviewFilter) (*TeamSection, error)

	ReadStages(ctx context.Context, workspaceID string, filter OverviewFilter) (OverviewStages, error)

	ReadBacklog(ctx context.Context, workspaceID string, filter OverviewFilter, now time.Time) (BacklogXray, error)

	ReadRework(ctx context.Context, workspaceID string, filter OverviewFilter) (OverviewRework, error)

	GetTrend(ctx context.Context, workspaceID string, filter OverviewFilter, buckets int, loc *time.Location) (TrendResult, error)

	GetRevenue(ctx context.Context, workspaceID string, from, to time.Time) ([]RevenueTally, int64, error)

	GetRevenueByMonth(ctx context.Context, workspaceID string, from, to time.Time, loc *time.Location, ownerID string) ([]RevenueMonthRow, error)
}
