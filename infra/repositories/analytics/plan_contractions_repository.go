package analytics_repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
)

const (
	defaultPlanContractionsRecentLimit = 10
	maxPlanContractionsRecentLimit     = 50
)

type planContractionCountRow struct {
	Key   string `gorm:"column:key"`
	Count int64  `gorm:"column:count"`
}

type planContractionBucketRow struct {
	Period time.Time `gorm:"column:period"`
	Count  int64     `gorm:"column:count"`
}

type planContractionRecentRow struct {
	SubscriptionID string    `gorm:"column:subscription_id"`
	WorkspaceID    string    `gorm:"column:workspace_id"`
	WorkspaceName  string    `gorm:"column:workspace_name"`
	PlanName       string    `gorm:"column:plan_name"`
	BillingCycle   string    `gorm:"column:billing_cycle"`
	Status         string    `gorm:"column:status"`
	ContractedAt   time.Time `gorm:"column:contracted_at"`
}

func (r *repository) GetPlanContractions(input analytics_domain.PlanContractionsInput) (*analytics_domain.PlanContractionsReport, error) {
	recentLimit := input.RecentLimit
	if recentLimit <= 0 {
		recentLimit = defaultPlanContractionsRecentLimit
	}
	if recentLimit > maxPlanContractionsRecentLimit {
		recentLimit = maxPlanContractionsRecentLimit
	}

	report := &analytics_domain.PlanContractionsReport{
		Period:         analytics_domain.Period{StartDate: input.StartDate, EndDate: input.EndDate},
		ByPlan:         []analytics_domain.PlanContractionCount{},
		ByBillingCycle: []analytics_domain.PlanContractionCount{},
		TimeSeries:     []analytics_domain.PlanContractionBucket{},
		Recent:         []analytics_domain.PlanContraction{},
	}

	base := func() *gorm.DB {
		return r.db.Table("workspace_subscriptions").
			Where("created_at >= ? AND created_at <= ?", input.StartDate, input.EndDate)
	}

	if err := base().Count(&report.TotalCount).Error; err != nil {
		return nil, fmt.Errorf("analytics plan contractions total: %w", err)
	}

	var planRows []planContractionCountRow
	if err := base().
		Select("plan_name as key, COUNT(*) as count").
		Group("plan_name").
		Order("count DESC, plan_name ASC").
		Scan(&planRows).Error; err != nil {
		return nil, fmt.Errorf("analytics plan contractions by plan: %w", err)
	}
	report.ByPlan = mapPlanContractionCounts(planRows)

	var cycleRows []planContractionCountRow
	if err := base().
		Select("billing_cycle as key, COUNT(*) as count").
		Group("billing_cycle").
		Order("count DESC, billing_cycle ASC").
		Scan(&cycleRows).Error; err != nil {
		return nil, fmt.Errorf("analytics plan contractions by cycle: %w", err)
	}
	report.ByBillingCycle = mapPlanContractionCounts(cycleRows)

	if input.Granularity != analytics_domain.GranularityTotal {
		truncExpr := fmt.Sprintf("DATE_TRUNC('%s', created_at)", dateTruncUnit(input.Granularity))
		var bucketRows []planContractionBucketRow
		if err := base().
			Select(fmt.Sprintf("%s as period, COUNT(*) as count", truncExpr)).
			Group(truncExpr).
			Order("period ASC").
			Scan(&bucketRows).Error; err != nil {
			return nil, fmt.Errorf("analytics plan contractions time series: %w", err)
		}
		for _, row := range bucketRows {
			report.TimeSeries = append(report.TimeSeries, analytics_domain.PlanContractionBucket{
				PeriodStart: row.Period,
				PeriodEnd:   bucketEnd(row.Period, input.Granularity),
				Count:       row.Count,
			})
		}
	}

	var recentRows []planContractionRecentRow
	if err := r.db.Table("workspace_subscriptions AS sub").
		Select(`sub.id AS subscription_id,
			sub.workspace_id AS workspace_id,
			w.name AS workspace_name,
			sub.plan_name AS plan_name,
			sub.billing_cycle AS billing_cycle,
			sub.status AS status,
			sub.created_at AS contracted_at`).
		Joins("JOIN workspaces w ON w.id = sub.workspace_id AND w.deleted_at IS NULL").
		Where("sub.created_at >= ? AND sub.created_at <= ?", input.StartDate, input.EndDate).
		Order("sub.created_at DESC").
		Limit(recentLimit).
		Scan(&recentRows).Error; err != nil {
		return nil, fmt.Errorf("analytics plan contractions recent: %w", err)
	}
	for _, row := range recentRows {
		report.Recent = append(report.Recent, analytics_domain.PlanContraction{
			SubscriptionID: row.SubscriptionID,
			WorkspaceID:    row.WorkspaceID,
			WorkspaceName:  row.WorkspaceName,
			PlanName:       row.PlanName,
			BillingCycle:   row.BillingCycle,
			Status:         row.Status,
			ContractedAt:   row.ContractedAt,
		})
	}

	return report, nil
}

func mapPlanContractionCounts(rows []planContractionCountRow) []analytics_domain.PlanContractionCount {
	items := make([]analytics_domain.PlanContractionCount, 0, len(rows))
	for _, row := range rows {
		items = append(items, analytics_domain.PlanContractionCount{
			Key:   row.Key,
			Count: row.Count,
		})
	}
	return items
}
