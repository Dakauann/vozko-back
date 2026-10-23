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

type TimelineRelation struct {
	From    string
	Scope   string
	Columns map[campaign.Milestone]string
}

func (t EntryTable) relation() TimelineRelation {
	columns := make(map[campaign.Milestone]string, len(campaign.AllMilestones()))
	for _, m := range campaign.AllMilestones() {
		columns[m] = "e." + column(m)
	}
	return TimelineRelation{
		From:    string(t) + " e",
		Scope:   "e.campaign_id = ? AND e.deleted_at IS NULL",
		Columns: columns,
	}
}

type TimelineReader struct {
	db       *gorm.DB
	relation TimelineRelation
}

func NewTimelineReader(db *gorm.DB, table EntryTable) *TimelineReader {
	return NewRelationTimelineReader(db, table.relation())
}

func NewRelationTimelineReader(db *gorm.DB, relation TimelineRelation) *TimelineReader {
	return &TimelineReader{db: db, relation: relation}
}

func (r *TimelineReader) Span(campaignID string) (campaign.TimelineSpan, error) {
	var row struct {
		First *time.Time
		Last  *time.Time
	}
	cols := r.relation.Columns
	query := fmt.Sprintf(
		"SELECT MIN(LEAST(%s, %s)) AS first, MAX(GREATEST(%s)) AS last FROM %s WHERE %s",
		cols[campaign.MilestoneSent], cols[campaign.MilestoneFailed], strings.Join(r.milestoneColumns(), ", "), r.relation.From, r.relation.Scope,
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
			"FROM %s CROSS JOIN LATERAL (VALUES %s) AS m(kind, at) "+
			"WHERE %s AND m.at >= ? AND m.at <= ? GROUP BY 1, 2 ORDER BY 1",
		r.relation.From, r.milestoneValues(), r.relation.Scope,
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

func (r *TimelineReader) milestoneColumns() []string {
	milestones := campaign.AllMilestones()
	columns := make([]string, 0, len(milestones))
	for _, m := range milestones {
		columns = append(columns, r.relation.Columns[m])
	}
	return columns
}

func (r *TimelineReader) milestoneValues() string {
	milestones := campaign.AllMilestones()
	values := make([]string, 0, len(milestones))
	for _, m := range milestones {
		values = append(values, fmt.Sprintf("('%s', %s)", m, r.relation.Columns[m]))
	}
	return strings.Join(values, ", ")
}
