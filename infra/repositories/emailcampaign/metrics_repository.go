package emailcampaign

import (
	"context"
	"time"

	"gorm.io/gorm"

	domain "vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/infra/database"
)

const (
	campaignMessagesSQL = "SELECT count(m.sent_at) AS sent," +
		" count(m.delivered_at) AS delivered, count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks," +
		" count(*) FILTER (WHERE m.bounce_kind = 'hard') AS hard_bounces, count(*) FILTER (WHERE m.bounce_kind = 'soft') AS soft_bounces," +
		" count(m.complained_at) AS complaints, count(m.unsubscribed_at) AS unsubscribes" +
		" FROM email_messages m WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id = ?"

	variantResultsSQL = "SELECT r.variant, count(m.delivered_at) AS delivered, count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks" +
		" FROM email_campaign_recipients r JOIN email_messages m ON m.id = r.message_id" +
		" WHERE r.workspace_id = ? AND r.campaign_id = ? AND r.variant IN ('a', 'b') GROUP BY r.variant"

	summariesSQL = "SELECT m.run_id AS campaign_id, count(m.sent_at) AS sent, count(m.delivered_at) AS delivered," +
		" count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks" +
		" FROM email_messages m WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id IN ? GROUP BY m.run_id"

	timelineSQL = "SELECT date_trunc(?, t.at AT TIME ZONE ?) AT TIME ZONE ? AS bucket," +
		" count(*) FILTER (WHERE t.kind = 'sent') AS sent, count(*) FILTER (WHERE t.kind = 'delivered') AS delivered," +
		" count(*) FILTER (WHERE t.kind = 'open') AS opens, count(*) FILTER (WHERE t.kind = 'click') AS clicks," +
		" count(*) FILTER (WHERE t.kind = 'bounce') AS bounces" +
		" FROM email_messages m CROSS JOIN LATERAL (VALUES ('sent', m.sent_at), ('delivered', m.delivered_at), ('open', m.first_open_at)," +
		" ('click', m.first_click_at), ('bounce', m.bounced_at)) AS t(kind, at)" +
		" WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id = ? AND t.at IS NOT NULL GROUP BY 1 ORDER BY 1"

	linksSQL = "SELECT e.link AS url, count(*) AS clicks, count(DISTINCT e.message_id) AS unique_clickers" +
		" FROM email_message_events e JOIN email_messages m ON m.id = e.message_id" +
		" WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id = ? AND e.kind = 'click' AND NOT e.is_bot AND COALESCE(e.link, '') <> ''" +
		" GROUP BY e.link ORDER BY clicks DESC, e.link LIMIT ?"

	domainsSQL = "SELECT split_part(m.address, '@', 2) AS domain, count(m.sent_at) AS sent, count(m.delivered_at) AS delivered," +
		" count(m.first_open_at) AS opens, count(m.bounced_at) AS bounces" +
		" FROM email_messages m WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id = ? GROUP BY 1"

	overviewTotalsSQL = "SELECT count(m.sent_at) AS sent," +
		" count(m.delivered_at) AS delivered, count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks," +
		" count(*) FILTER (WHERE m.bounce_kind = 'hard') AS hard_bounces, count(*) FILTER (WHERE m.bounce_kind = 'soft') AS soft_bounces," +
		" count(m.complained_at) AS complaints, count(m.unsubscribed_at) AS unsubscribes" +
		" FROM email_messages m WHERE m.workspace_id = ? AND m.run_kind IN ('campaign', 'triggered') AND m.sent_at >= ?"

	overviewDailySQL = "SELECT date_trunc('day', m.sent_at AT TIME ZONE ?) AT TIME ZONE ? AS bucket, count(*) AS sent, count(m.delivered_at) AS delivered," +
		" count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks, count(m.bounced_at) AS bounces" +
		" FROM email_messages m WHERE m.workspace_id = ? AND m.run_kind IN ('campaign', 'triggered') AND m.sent_at >= ?" +
		" GROUP BY 1 ORDER BY 1"

	overviewTopSQL = "SELECT c.id, c.name, count(*) AS sent, count(m.delivered_at) AS delivered," +
		" count(m.first_open_at) AS opens, count(m.first_click_at) AS clicks" +
		" FROM email_messages m JOIN email_campaigns c ON c.id::text = m.run_id::text AND c.workspace_id = m.workspace_id" +
		" WHERE m.workspace_id = ? AND m.run_kind = 'campaign' AND m.sent_at >= ?" +
		" GROUP BY c.id, c.name ORDER BY sent DESC, c.name LIMIT ?"
)

type MetricsRepository struct {
	db         *gorm.DB
	recipients *RecipientRepository
}

func NewMetricsRepository(db *gorm.DB) *MetricsRepository {
	return &MetricsRepository{db: db, recipients: NewRecipientRepository(db)}
}

type messageTotals struct {
	Sent         int64
	Delivered    int64
	Opens        int64
	Clicks       int64
	HardBounces  int64
	SoftBounces  int64
	Complaints   int64
	Unsubscribes int64
}

func (t messageTotals) into(tally *domain.Tally) {
	tally.Delivered, tally.Opens, tally.Clicks = t.Delivered, t.Opens, t.Clicks
	tally.HardBounces, tally.SoftBounces = t.HardBounces, t.SoftBounces
	tally.Complaints, tally.Unsubscribes = t.Complaints, t.Unsubscribes
}

func (r *MetricsRepository) Tally(ctx context.Context, workspaceID, campaignID string) (domain.Tally, error) {
	counts, err := r.recipients.Counts(ctx, campaignID)
	if err != nil {
		return domain.Tally{}, err
	}
	var totals messageTotals
	if err := r.db.WithContext(ctx).Raw(campaignMessagesSQL, workspaceID, campaignID).Scan(&totals).Error; err != nil {
		return domain.Tally{}, err
	}
	tally := domain.Tally{Recipients: counts.Total, Pending: counts.Pending, Sent: totals.Sent, Failed: counts.Failed, Skipped: counts.Skipped}
	totals.into(&tally)
	return tally, nil
}

func (r *MetricsRepository) Summaries(ctx context.Context, workspaceID string, campaignIDs []string) (map[string]domain.Tally, error) {
	valid := database.UUIDArray(campaignIDs)
	out := make(map[string]domain.Tally, len(valid))
	if len(valid) == 0 {
		return out, nil
	}
	var rows []struct {
		CampaignID string
		Sent       int64
		Delivered  int64
		Opens      int64
		Clicks     int64
	}
	if err := r.db.WithContext(ctx).Raw(summariesSQL, workspaceID, []string(valid)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.CampaignID] = domain.Tally{Sent: row.Sent, Delivered: row.Delivered, Opens: row.Opens, Clicks: row.Clicks}
	}
	return out, nil
}

func (r *MetricsRepository) VariantResults(ctx context.Context, workspaceID, campaignID string) (map[domain.Variant]domain.VariantResult, error) {
	var rows []struct {
		Variant   string
		Delivered int64
		Opens     int64
		Clicks    int64
	}
	if err := r.db.WithContext(ctx).Raw(variantResultsSQL, workspaceID, campaignID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[domain.Variant]domain.VariantResult{}
	for _, row := range rows {
		out[domain.Variant(row.Variant)] = domain.VariantResult{Delivered: row.Delivered, Opens: row.Opens, Clicks: row.Clicks}
	}
	return out, nil
}

type timelineRow struct {
	Bucket    time.Time
	Sent      int64
	Delivered int64
	Opens     int64
	Clicks    int64
	Bounces   int64
}

func timelineOf(rows []timelineRow) []domain.TimelinePoint {
	out := make([]domain.TimelinePoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.TimelinePoint{Bucket: row.Bucket.UTC(), Sent: row.Sent, Delivered: row.Delivered, Opens: row.Opens, Clicks: row.Clicks, Bounces: row.Bounces})
	}
	return out
}

func (r *MetricsRepository) Timeline(ctx context.Context, workspaceID, campaignID string, bucket domain.Bucket) ([]domain.TimelinePoint, error) {
	var rows []timelineRow
	zone := emailsending.TimeZoneName
	if err := r.db.WithContext(ctx).Raw(timelineSQL, string(bucket), zone, zone, workspaceID, campaignID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return timelineOf(rows), nil
}

func (r *MetricsRepository) Links(ctx context.Context, workspaceID, campaignID string, limit int) ([]domain.LinkStat, error) {
	var rows []domain.LinkStat
	if err := r.db.WithContext(ctx).Raw(linksSQL, workspaceID, campaignID, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.LinkStat{}
	}
	return rows, nil
}

func (r *MetricsRepository) Domains(ctx context.Context, workspaceID, campaignID string) ([]domain.DomainCount, error) {
	var rows []domain.DomainCount
	if err := r.db.WithContext(ctx).Raw(domainsSQL, workspaceID, campaignID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MetricsRepository) Overview(ctx context.Context, workspaceID string, since time.Time, top int) (domain.OverviewData, error) {
	db := r.db.WithContext(ctx)
	var totals messageTotals
	if err := db.Raw(overviewTotalsSQL, workspaceID, since.UTC()).Scan(&totals).Error; err != nil {
		return domain.OverviewData{}, err
	}
	data := domain.OverviewData{Totals: domain.Tally{Sent: totals.Sent}}
	totals.into(&data.Totals)
	var daily []timelineRow
	zone := emailsending.TimeZoneName
	if err := db.Raw(overviewDailySQL, zone, zone, workspaceID, since.UTC()).Scan(&daily).Error; err != nil {
		return domain.OverviewData{}, err
	}
	data.Daily = timelineOf(daily)
	var tops []domain.CampaignSummary
	if err := db.Raw(overviewTopSQL, workspaceID, since.UTC(), top).Scan(&tops).Error; err != nil {
		return domain.OverviewData{}, err
	}
	for i := range tops {
		tops[i].OpenRate = domain.BasisPoints(tops[i].Opens, tops[i].Delivered)
		tops[i].ClickRate = domain.BasisPoints(tops[i].Clicks, tops[i].Delivered)
	}
	if tops == nil {
		tops = []domain.CampaignSummary{}
	}
	data.Top = tops
	return data, nil
}

var _ domain.MetricsReader = (*MetricsRepository)(nil)
