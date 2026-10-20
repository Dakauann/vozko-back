package campaignstamp

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/campaign"
)

type EntryTable string

const (
	OfficialEntries   EntryTable = "whatsapp_campaign_entries"
	UnofficialEntries EntryTable = "unofficial_whatsapp_campaign_entries"
)

type TimelineReader struct {
	db    *gorm.DB
	table EntryTable
}

func NewTimelineReader(db *gorm.DB, table EntryTable) *TimelineReader {
	return &TimelineReader{db: db, table: table}
}

func (r *TimelineReader) Span(campaignID string) (campaign.TimelineSpan, error) {
	var row struct {
		First *time.Time
		Last  *time.Time
	}
	query := fmt.Sprintf(
		"SELECT MIN(LEAST(%s, %s)) AS first, MAX(GREATEST(%s)) AS last FROM %s WHERE campaign_id = ? AND deleted_at IS NULL",
		column(campaign.MilestoneSent), column(campaign.MilestoneFailed), strings.Join(milestoneColumns(), ", "), r.table,
	)
	if err := r.db.Raw(query, campaignID).Scan(&row).Error; err != nil {
		return campaign.TimelineSpan{}, err
	}
	return campaign.TimelineSpan{First: row.First, Last: row.Last}, nil
}

func (r *TimelineReader) Count(campaignID string, plan campaign.TimelinePlan) ([]campaign.TimelinePoint, error) {
	var rows []struct {
		Bucket time.Time
		Kind   string
		Total  int64
	}
	query := fmt.Sprintf(
		"SELECT date_trunc(?, m.at AT TIME ZONE ?) AT TIME ZONE ? AS bucket, m.kind, COUNT(*) AS total "+
			"FROM %s e CROSS JOIN LATERAL (VALUES %s) AS m(kind, at) "+
			"WHERE e.campaign_id = ? AND e.deleted_at IS NULL AND m.at >= ? AND m.at <= ? GROUP BY 1, 2 ORDER BY 1",
		r.table, milestoneValues(),
	)
	zone := plan.Location.String()
	if err := r.db.Raw(query, string(plan.Bucket), zone, zone, campaignID, plan.From, plan.To).Scan(&rows).Error; err != nil {
		return nil, err
	}

	var points []campaign.TimelinePoint
	for _, row := range rows {
		if len(points) == 0 || !points[len(points)-1].At.Equal(row.Bucket) {
			points = append(points, campaign.TimelinePoint{At: row.Bucket})
		}
		points[len(points)-1].Add(campaign.Milestone(row.Kind), row.Total)
	}
	return points, nil
}

func milestoneColumns() []string {
	milestones := campaign.AllMilestones()
	columns := make([]string, 0, len(milestones))
	for _, m := range milestones {
		columns = append(columns, column(m))
	}
	return columns
}

func milestoneValues() string {
	milestones := campaign.AllMilestones()
	values := make([]string, 0, len(milestones))
	for _, m := range milestones {
		values = append(values, fmt.Sprintf("('%s', e.%s)", m, column(m)))
	}
	return strings.Join(values, ", ")
}
