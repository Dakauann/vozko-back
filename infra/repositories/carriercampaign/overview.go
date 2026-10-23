package carriercampaign_repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
	ccuc "vozko/usecases/carriercampaign"
)

var errOverviewQuery = errors.New("carriercampaign: invalid overview query")

type OverviewReader struct {
	db          *gorm.DB
	attribution attribution
}

var _ ccuc.OverviewReader = (*OverviewReader)(nil)

func NewOverviewReader(db *gorm.DB) *OverviewReader {
	return &OverviewReader{db: db, attribution: attribution{db: db}}
}

const settledLedgerRows = `((type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true))`

const overviewFactsSQL = `
WITH facts AS (
	SELECT m.channel, f.kind, f.at
	FROM carrier_messages m
	CROSS JOIN LATERAL (VALUES
		('sent', m.created_at), ('accepted', m.accepted_at), ('delivered', m.delivered_at), ('read', m.read_at),
		(CASE m.status WHEN 'rejected' THEN 'rejected' WHEN 'expired' THEN 'expired' ELSE 'failed' END, m.failed_at)
	) AS f(kind, at)
	WHERE m.workspace_id = ? AND m.parent_message_id IS NULL
	UNION ALL
	SELECT COALESCE(gp.channel, p.channel), CASE c.step WHEN 'whatsapp' THEN 'fell_back_whatsapp' ELSE 'fell_back_sms' END, c.created_at
	FROM carrier_messages c
	JOIN carrier_messages p ON p.id = c.parent_message_id
	LEFT JOIN carrier_messages gp ON gp.id = p.parent_message_id
	WHERE c.workspace_id = ? AND c.step IN ('whatsapp', 'sms')
	UNION ALL
	SELECT COALESCE(em.channel,
			CASE WHEN EXISTS (SELECT 1 FROM rcs_senders s WHERE s.id = e.sender_id) THEN 'rcs'
				WHEN EXISTS (SELECT 1 FROM sms_routes sr WHERE sr.id = e.sender_id) THEN 'sms' END),
		CASE e.kind WHEN 'tap' THEN 'taps' WHEN 'click' THEN 'clicks' WHEN 'inbound' THEN 'replies' WHEN 'opt_out' THEN 'opt_outs' ELSE e.kind END,
		e.occurred_at
	FROM carrier_message_events e
	LEFT JOIN carrier_messages em ON em.id = e.message_id
	WHERE e.workspace_id = ?
	UNION ALL
	SELECT cc.channel, f.kind, f.at
	FROM carrier_campaign_recipients r
	JOIN carrier_campaigns cc ON cc.id = r.campaign_id
	CROSS JOIN LATERAL (VALUES ('targeted', r.created_at), ('skipped', CASE WHEN r.status = 'skipped' THEN r.created_at END)) AS f(kind, at)
	WHERE r.workspace_id = ?
)
SELECT to_char(date_trunc('day', at AT TIME ZONE ?), 'YYYY-MM-DD') AS day, channel, kind, COUNT(*) AS total
FROM facts
WHERE at >= ? AND at < ?
GROUP BY 1, 2, 3
ORDER BY 1, 2`

const overviewSpendSQL = `
SELECT to_char(date_trunc('day', created_at AT TIME ZONE ?), 'YYYY-MM-DD') AS day, service_type AS channel,
	SUM(CASE WHEN type = 'debit' THEN amount ELSE -amount END) AS total
FROM balance_transactions
WHERE workspace_id = ? AND service_type IN ? AND created_at >= ? AND created_at < ? AND ` + settledLedgerRows + `
GROUP BY 1, 2`

type overviewRow struct {
	Day     string
	Channel sql.NullString
	Kind    string
	Total   int64
}

func overviewServices(channel carriermessaging.Channel) []string {
	if channel != "" {
		return []string{string(channel)}
	}
	return carrierLedgerServices
}

func overviewChannels(channel carriermessaging.Channel) []carriermessaging.Channel {
	if channel != "" {
		return []carriermessaging.Channel{channel}
	}
	return carriermessaging.AllChannels()
}

func (r *OverviewReader) Overview(ctx context.Context, workspaceID string, from, to time.Time, tz string, channel carriermessaging.Channel) (ccuc.Overview, error) {
	if channel != "" {
		if err := valid("channel", channel); err != nil {
			return ccuc.Overview{}, err
		}
	}
	if _, err := time.LoadLocation(tz); err != nil || tz == "" {
		return ccuc.Overview{}, fmt.Errorf("%w: unknown time zone %q", errOverviewQuery, tz)
	}
	if !from.Before(to) {
		return ccuc.Overview{}, fmt.Errorf("%w: the range must start before it ends", errOverviewQuery)
	}
	var facts, spend []overviewRow
	if err := r.db.WithContext(ctx).Raw(overviewFactsSQL, workspaceID, workspaceID, workspaceID, workspaceID, tz, from, to).Scan(&facts).Error; err != nil {
		return ccuc.Overview{}, err
	}
	if err := r.db.WithContext(ctx).Raw(overviewSpendSQL, tz, workspaceID, overviewServices(channel), from, to).Scan(&spend).Error; err != nil {
		return ccuc.Overview{}, err
	}
	for i := range spend {
		spend[i].Kind = "spend_micros"
	}
	out, err := assembleOverview(channel, append(facts, spend...))
	if err != nil {
		return ccuc.Overview{}, err
	}
	conversions, err := r.conversions(ctx, workspaceID, from, to)
	if err != nil {
		return ccuc.Overview{}, err
	}
	spendBy := map[carriermessaging.Channel]int64{}
	for _, d := range out.Daily {
		spendBy[d.Channel] += d.SpendMicros
	}
	for _, c := range overviewChannels(channel) {
		won := conversions[c]
		out.Totals.Conversions += won.count
		out.Totals.ConversionValueMicros += won.valueMicros
		out.Channels = append(out.Channels, ccuc.ChannelTotals{Channel: c, SpendMicros: spendBy[c], Conversions: won.count})
	}
	if out.Carriers, err = r.carriers(ctx, workspaceID, from, to, channel); err != nil {
		return ccuc.Overview{}, err
	}
	return out, nil
}

type conversionTally struct {
	count       int64
	valueMicros int64
}

func (r *OverviewReader) conversions(ctx context.Context, workspaceID string, from, to time.Time) (map[carriermessaging.Channel]conversionTally, error) {
	days, err := r.attribution.days(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	deals, err := r.attribution.dealsWonBetween(ctx, workspaceID, from, to)
	if err != nil {
		return nil, err
	}
	wins, err := r.attribution.lastTouches(ctx, workspaceID, deals, days)
	if err != nil {
		return nil, err
	}
	out := map[carriermessaging.Channel]conversionTally{}
	for _, w := range wins {
		c := carriermessaging.Channel(w.touch.Channel)
		if !c.Valid() {
			continue
		}
		tally := out[c]
		tally.count++
		tally.valueMicros += w.deal.ValueCents * centsToMicros
		out[c] = tally
	}
	return out, nil
}

func (r *OverviewReader) carriers(ctx context.Context, workspaceID string, from, to time.Time, channel carriermessaging.Channel) ([]carriercampaign.CarrierStat, error) {
	where, args := "t.workspace_id = ? AND t.parent_message_id IS NULL AND t.created_at >= ? AND t.created_at < ?", []any{workspaceID, from, to}
	if channel != "" {
		where, args = where+" AND t.channel = ?", append(args, channel)
	}
	var rows []carrierRow
	if err := r.db.WithContext(ctx).Raw(carrierColumnsSQL+`
FROM carrier_messages t WHERE `+where+`
GROUP BY 1 ORDER BY sent DESC, operator ASC`, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return toCarrierStats(rows), nil
}

func assembleOverview(filter carriermessaging.Channel, rows []overviewRow) (ccuc.Overview, error) {
	type key struct {
		day     string
		channel carriermessaging.Channel
	}
	days := map[key]*ccuc.OverviewDay{}
	var out ccuc.Overview
	for _, row := range rows {
		channel := carriermessaging.Channel(row.Channel.String)
		if err := valid("overview channel", channel); err != nil {
			return ccuc.Overview{}, fmt.Errorf("%s %s: %w", row.Day, row.Kind, err)
		}
		if filter != "" && channel != filter {
			continue
		}
		date, err := shared.ParseDate(row.Day)
		if err != nil {
			return ccuc.Overview{}, err
		}
		k := key{row.Day, channel}
		day, ok := days[k]
		daily := func() *ccuc.OverviewDay {
			if !ok {
				day = &ccuc.OverviewDay{Day: date, Channel: channel}
				days[k], ok = day, true
			}
			return day
		}
		t := &out.Totals
		switch row.Kind {
		case "sent":
			t.Sent += row.Total
			daily().Sent += row.Total
		case "accepted":
			t.Accepted += row.Total
		case "delivered":
			t.Delivered += row.Total
			daily().Delivered += row.Total
		case "read":
			t.Read += row.Total
			daily().Read += row.Total
		case "failed":
			t.Failed += row.Total
			daily().Failed += row.Total
		case "rejected":
			t.Rejected += row.Total
			daily().Failed += row.Total
		case "expired":
			t.Expired += row.Total
			daily().Failed += row.Total
		case "fell_back_whatsapp":
			t.FellBackWhatsApp += row.Total
		case "fell_back_sms":
			t.FellBackSMS += row.Total
		case "taps":
			t.Taps += row.Total
		case "clicks":
			t.Clicks += row.Total
		case "replies":
			t.Replies += row.Total
		case "opt_outs":
			t.OptOuts += row.Total
		case "targeted":
			t.Targeted += row.Total
		case "skipped":
			t.Skipped += row.Total
		case "spend_micros":
			t.SpendMicros += row.Total
			daily().SpendMicros += row.Total
		default:
			return ccuc.Overview{}, fmt.Errorf("%w: overview kind %q", errUnknownStoredValue, row.Kind)
		}
	}
	out.Daily = make([]ccuc.OverviewDay, 0, len(days))
	for _, d := range days {
		out.Daily = append(out.Daily, *d)
	}
	sort.Slice(out.Daily, func(i, j int) bool {
		a, b := out.Daily[i], out.Daily[j]
		if !a.Day.Time().Equal(b.Day.Time()) {
			return a.Day.Time().Before(b.Day.Time())
		}
		return a.Channel < b.Channel
	})
	return out, nil
}
