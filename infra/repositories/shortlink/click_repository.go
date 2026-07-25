package shortlink

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
	"vozko/infra/database/schema"
)

type clickRepository struct {
	db *gorm.DB
}

func NewClickRepository(db *gorm.DB) shortlink.ClickRepository {
	return &clickRepository{db: db}
}

func (r *clickRepository) RecordClick(ctx context.Context, click *shortlink.Click) (bool, error) {
	row := mapClickToSchema(click)
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *clickRepository) ApplyDailyStats(ctx context.Context, deltas []shortlink.DailyStatDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range deltas {
			row := schema.ShortLinkDailyStat{
				ID:             uuid.New().String(),
				ShortLinkID:    d.ShortLinkID,
				WorkspaceID:    d.WorkspaceID,
				Day:            d.Day,
				Dimension:      d.Dimension,
				DimensionValue: d.DimensionValue,
				Clicks:         d.Clicks,
				UniqueClicks:   d.UniqueClicks,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "short_link_id"},
					{Name: "day"},
					{Name: "dimension"},
					{Name: "dimension_value"},
				},
				DoUpdates: clause.Assignments(map[string]any{
					"clicks":        gorm.Expr("short_link_daily_stats.clicks + ?", d.Clicks),
					"unique_clicks": gorm.Expr("short_link_daily_stats.unique_clicks + ?", d.UniqueClicks),
					"updated_at":    time.Now(),
				}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type dailyAggregateRow struct {
	Total  int64
	Unique int64
}

type timeSeriesRow struct {
	Day    time.Time
	Clicks int64
}

type dimensionRow struct {
	Value  string
	Clicks int64
}

func (r *clickRepository) Analytics(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
	analytics := &shortlink.Analytics{
		TimeSeries: []shortlink.TimePoint{},
		ByCountry:  []shortlink.DimensionCount{},
		ByDevice:   []shortlink.DimensionCount{},
		ByReferer:  []shortlink.DimensionCount{},
		ByBrowser:  []shortlink.DimensionCount{},
		ByOS:       []shortlink.DimensionCount{},
	}

	var totals dailyAggregateRow
	if err := r.db.WithContext(ctx).
		Model(&schema.ShortLinkDailyStat{}).
		Select("COALESCE(SUM(clicks), 0) AS total, COALESCE(SUM(unique_clicks), 0) AS unique").
		Where("short_link_id = ? AND dimension = ? AND day BETWEEN ? AND ?", in.ShortLinkID, shortlink.DimTotal, in.From, in.To).
		Scan(&totals).Error; err != nil {
		return nil, err
	}
	analytics.TotalClicks = totals.Total
	analytics.UniqueClicks = totals.Unique

	var series []timeSeriesRow
	if err := r.db.WithContext(ctx).
		Model(&schema.ShortLinkDailyStat{}).
		Select("day AS day, COALESCE(SUM(clicks), 0) AS clicks").
		Where("short_link_id = ? AND dimension = ? AND day BETWEEN ? AND ?", in.ShortLinkID, shortlink.DimTotal, in.From, in.To).
		Group("day").
		Order("day ASC").
		Scan(&series).Error; err != nil {
		return nil, err
	}
	for _, s := range series {
		analytics.TimeSeries = append(analytics.TimeSeries, shortlink.TimePoint{
			Date:   s.Day.UTC().Format("2006-01-02"),
			Clicks: s.Clicks,
		})
	}

	dimensions := []struct {
		name string
		dest *[]shortlink.DimensionCount
	}{
		{shortlink.DimCountry, &analytics.ByCountry},
		{shortlink.DimDevice, &analytics.ByDevice},
		{shortlink.DimReferer, &analytics.ByReferer},
		{shortlink.DimBrowser, &analytics.ByBrowser},
		{shortlink.DimOS, &analytics.ByOS},
	}
	for _, dim := range dimensions {
		rows, err := r.breakdown(ctx, in, dim.name)
		if err != nil {
			return nil, err
		}
		*dim.dest = rows
	}

	return analytics, nil
}

const breakdownLimit = 20

func (r *clickRepository) breakdown(ctx context.Context, in shortlink.AnalyticsInput, dimension string) ([]shortlink.DimensionCount, error) {
	var rows []dimensionRow
	if err := r.db.WithContext(ctx).
		Model(&schema.ShortLinkDailyStat{}).
		Select("dimension_value AS value, COALESCE(SUM(clicks), 0) AS clicks").
		Where("short_link_id = ? AND dimension = ? AND day BETWEEN ? AND ?", in.ShortLinkID, dimension, in.From, in.To).
		Group("dimension_value").
		Order("clicks DESC").
		Limit(breakdownLimit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]shortlink.DimensionCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, shortlink.DimensionCount{Label: row.Value, Clicks: row.Clicks})
	}
	return out, nil
}

func (r *clickRepository) RecentClicks(ctx context.Context, workspaceID, shortLinkID string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.Click], error) {
	pagination := shared.NormalizePagination(opts)

	var total int64
	if err := r.db.WithContext(ctx).
		Model(&schema.ShortLinkClick{}).
		Where("workspace_id = ? AND short_link_id = ?", workspaceID, shortLinkID).
		Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []schema.ShortLinkClick
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND short_link_id = ?", workspaceID, shortLinkID).
		Order("occurred_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*shortlink.Click, 0, len(rows))
	for i := range rows {
		items = append(items, mapClickToDomain(&rows[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *clickRepository) PurgeClicksBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("occurred_at < ?", cutoff).Delete(&schema.ShortLinkClick{})
	return res.RowsAffected, res.Error
}

func (r *clickRepository) PurgeDailyStatsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("day < ?", cutoff).Delete(&schema.ShortLinkDailyStat{})
	return res.RowsAffected, res.Error
}

func mapClickToSchema(click *shortlink.Click) *schema.ShortLinkClick {
	return &schema.ShortLinkClick{
		ID:            click.ID,
		ShortLinkID:   click.ShortLinkID,
		WorkspaceID:   click.WorkspaceID,
		OccurredAt:    click.OccurredAt,
		IPHash:        click.IPHash,
		Country:       click.Country,
		Region:        click.Region,
		City:          click.City,
		DeviceType:    click.DeviceType,
		OS:            click.OS,
		Browser:       click.Browser,
		RefererDomain: click.RefererDomain,
		UTMSource:     click.UTMSource,
		UTMMedium:     click.UTMMedium,
		UTMCampaign:   click.UTMCampaign,
		IsBot:         click.IsBot,
		IsProxy:       click.IsProxy,
		Language:      click.Language,
	}
}

func mapClickToDomain(row *schema.ShortLinkClick) *shortlink.Click {
	return &shortlink.Click{
		ID:            row.ID,
		ShortLinkID:   row.ShortLinkID,
		WorkspaceID:   row.WorkspaceID,
		OccurredAt:    row.OccurredAt,
		IPHash:        row.IPHash,
		Country:       row.Country,
		Region:        row.Region,
		City:          row.City,
		DeviceType:    row.DeviceType,
		OS:            row.OS,
		Browser:       row.Browser,
		RefererDomain: row.RefererDomain,
		UTMSource:     row.UTMSource,
		UTMMedium:     row.UTMMedium,
		UTMCampaign:   row.UTMCampaign,
		IsBot:         row.IsBot,
		IsProxy:       row.IsProxy,
		Language:      row.Language,
	}
}
