package carriercampaign_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/balance"
	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/opportunity"
	"vozko/domain/rcs"
)

var errFunnelScope = errors.New("carriercampaign: a funnel needs a workspace")

var carrierLedgerServices = []string{string(balance.ServiceRCS), string(balance.ServiceSMS)}

type FunnelReader struct {
	db          *gorm.DB
	attribution attribution
}

var _ carriercampaign.FunnelReader = (*FunnelReader)(nil)

func NewFunnelReader(db *gorm.DB) *FunnelReader {
	return &FunnelReader{db: db, attribution: attribution{db: db}}
}

type funnelScope struct {
	workspaceID string
	campaignIDs []string
	runID       string
	channel     carriermessaging.Channel
}

func scopeOf(q carriercampaign.FunnelQuery) funnelScope {
	s := funnelScope{workspaceID: q.WorkspaceID, runID: q.RunID, channel: q.Channel}
	if q.CampaignID != "" {
		s.campaignIDs = []string{q.CampaignID}
	}
	return s
}

func (s funnelScope) cte() (string, []any, error) {
	if strings.TrimSpace(s.workspaceID) == "" {
		return "", nil, errFunnelScope
	}
	conds, args := []string{"r.workspace_id = ?"}, []any{s.workspaceID}
	if len(s.campaignIDs) > 0 {
		for _, id := range s.campaignIDs {
			if !wellFormedID(id) {
				return "", nil, carriercampaign.ErrCampaignNotFound
			}
		}
		conds, args = append(conds, "r.campaign_id IN ?"), append(args, s.campaignIDs)
	}
	if s.runID != "" {
		if !wellFormedID(s.runID) {
			return "", nil, carriercampaign.ErrRunNotFound
		}
		conds, args = append(conds, "r.run_id = ?"), append(args, s.runID)
	}
	if s.channel != "" {
		if err := valid("channel", s.channel); err != nil {
			return "", nil, err
		}
		conds, args = append(conds, "c.channel = ?"), append(args, s.channel)
	}
	return `WITH scope AS (
	SELECT r.campaign_id, r.run_id, r.lead_id, r.variant, r.status, r.message_id
	FROM carrier_campaign_recipients r
	JOIN carrier_campaigns c ON c.id = r.campaign_id
	WHERE ` + strings.Join(conds, " AND ") + `
),
tops AS (
	SELECT s.campaign_id, s.variant, m.id, m.status, m.accepted_at, m.delivered_at, m.read_at, m.operator, m.fallback_message_id, m.charge_reference
	FROM scope s
	JOIN carrier_messages m ON m.id = s.message_id
)`, args, nil
}

func (s funnelScope) holds(t touch) bool {
	if t.CampaignID == "" || t.Channel == whatsAppChannel {
		return false
	}
	inCampaigns := len(s.campaignIDs) == 0
	for _, id := range s.campaignIDs {
		if id == t.CampaignID {
			inCampaigns = true
		}
	}
	return inCampaigns && (s.runID == "" || t.RunID == s.runID) && (s.channel == "" || carriermessaging.Channel(t.Channel) == s.channel)
}

const funnelSQL = `,
kids AS (
	SELECT t.id AS top_id, c.step, c.charge_reference
	FROM tops t JOIN carrier_messages c ON c.parent_message_id = t.id
	UNION ALL
	SELECT t.id, g.step, g.charge_reference
	FROM tops t JOIN carrier_messages c ON c.parent_message_id = t.id JOIN carrier_messages g ON g.parent_message_id = c.id
),
refs AS (
	SELECT id AS top_id, charge_reference AS ref FROM tops WHERE charge_reference <> ''
	UNION ALL
	SELECT top_id, charge_reference FROM kids WHERE charge_reference <> ''
),
recipient_counts AS (
	SELECT campaign_id, variant, COUNT(*) AS targeted, COUNT(*) FILTER (WHERE status = 'skipped') AS skipped, COUNT(message_id) AS sent
	FROM scope GROUP BY campaign_id, variant
),
message_counts AS (
	SELECT t.campaign_id, t.variant,
		COUNT(*) FILTER (WHERE t.accepted_at IS NOT NULL) AS accepted,
		COUNT(*) FILTER (WHERE t.status = 'rejected') AS rejected,
		COUNT(*) FILTER (WHERE t.delivered_at IS NOT NULL) AS delivered,
		COUNT(*) FILTER (WHERE t.read_at IS NOT NULL) AS read,
		COUNT(*) FILTER (WHERE t.status = 'failed') AS failed,
		COUNT(*) FILTER (WHERE t.status = 'expired') AS expired,
		COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM kids k WHERE k.top_id = t.id AND k.step = 'whatsapp')) AS fell_back_whatsapp,
		COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM kids k WHERE k.top_id = t.id AND k.step = 'sms')) AS fell_back_sms
	FROM tops t GROUP BY t.campaign_id, t.variant
),
event_counts AS (
	SELECT t.campaign_id, t.variant,
		COUNT(DISTINCT e.message_id) FILTER (WHERE e.kind = 'tap') AS taps,
		COUNT(DISTINCT e.message_id) FILTER (WHERE e.kind = 'click') AS clicks,
		COUNT(DISTINCT e.message_id) FILTER (WHERE e.kind = 'inbound') AS replies,
		COUNT(DISTINCT e.message_id) FILTER (WHERE e.kind = 'opt_out') AS opt_outs
	FROM tops t JOIN carrier_message_events e ON e.message_id = t.id
	GROUP BY t.campaign_id, t.variant
),
spend AS (
	SELECT t.campaign_id, t.variant, SUM(CASE WHEN b.type = 'debit' THEN b.amount ELSE -b.amount END) AS spend_micros
	FROM refs
	JOIN tops t ON t.id = refs.top_id
	JOIN balance_transactions b ON b.workspace_id = ? AND b.service_type IN ?
		AND ((b.type = 'debit' AND b.is_refund = false AND b.reference_id = refs.ref)
			OR (b.type = 'credit' AND b.is_refund = true AND b.reference_id = 'refund:' || refs.ref))
	GROUP BY t.campaign_id, t.variant
)
SELECT rc.campaign_id, rc.variant, rc.targeted, rc.skipped, rc.sent,
	COALESCE(mc.accepted, 0) AS accepted, COALESCE(mc.rejected, 0) AS rejected, COALESCE(mc.delivered, 0) AS delivered,
	COALESCE(mc.read, 0) AS read, COALESCE(mc.failed, 0) AS failed, COALESCE(mc.expired, 0) AS expired,
	COALESCE(ec.taps, 0) AS taps, COALESCE(ec.clicks, 0) AS clicks, COALESCE(ec.replies, 0) AS replies, COALESCE(ec.opt_outs, 0) AS opt_outs,
	COALESCE(mc.fell_back_whatsapp, 0) AS fell_back_whatsapp, COALESCE(mc.fell_back_sms, 0) AS fell_back_sms,
	COALESCE(sp.spend_micros, 0) AS spend_micros
FROM recipient_counts rc
LEFT JOIN message_counts mc ON mc.campaign_id = rc.campaign_id AND mc.variant = rc.variant
LEFT JOIN event_counts ec ON ec.campaign_id = rc.campaign_id AND ec.variant = rc.variant
LEFT JOIN spend sp ON sp.campaign_id = rc.campaign_id AND sp.variant = rc.variant
ORDER BY rc.campaign_id, rc.variant`

type funnelRow struct {
	CampaignID       string `gorm:"column:campaign_id"`
	Variant          int    `gorm:"column:variant"`
	Targeted         int64  `gorm:"column:targeted"`
	Skipped          int64  `gorm:"column:skipped"`
	Sent             int64  `gorm:"column:sent"`
	Accepted         int64  `gorm:"column:accepted"`
	Rejected         int64  `gorm:"column:rejected"`
	Delivered        int64  `gorm:"column:delivered"`
	Read             int64  `gorm:"column:read"`
	Failed           int64  `gorm:"column:failed"`
	Expired          int64  `gorm:"column:expired"`
	Taps             int64  `gorm:"column:taps"`
	Clicks           int64  `gorm:"column:clicks"`
	Replies          int64  `gorm:"column:replies"`
	OptOuts          int64  `gorm:"column:opt_outs"`
	FellBackWhatsApp int64  `gorm:"column:fell_back_whatsapp"`
	FellBackSMS      int64  `gorm:"column:fell_back_sms"`
	SpendMicros      int64  `gorm:"column:spend_micros"`
}

func (r funnelRow) funnel() carriercampaign.Funnel {
	return carriercampaign.Funnel{
		Targeted: r.Targeted, Skipped: r.Skipped, Sent: r.Sent, Accepted: r.Accepted, Rejected: r.Rejected,
		Delivered: r.Delivered, Read: r.Read, Failed: r.Failed, Expired: r.Expired, Taps: r.Taps, Clicks: r.Clicks,
		Replies: r.Replies, OptOuts: r.OptOuts, FellBackWhatsApp: r.FellBackWhatsApp, FellBackSMS: r.FellBackSMS,
		SpendMicros: r.SpendMicros,
	}
}

type funnelCell struct {
	campaignID string
	variant    int
}

func (r *FunnelReader) cells(ctx context.Context, s funnelScope) (map[funnelCell]carriercampaign.Funnel, error) {
	cte, args, err := s.cte()
	if err != nil {
		return nil, err
	}
	var rows []funnelRow
	if err := r.db.WithContext(ctx).Raw(cte+funnelSQL, append(args, s.workspaceID, carrierLedgerServices)...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[funnelCell]carriercampaign.Funnel, len(rows))
	for _, row := range rows {
		out[funnelCell{row.CampaignID, row.Variant}] = row.funnel()
	}
	days, err := r.attribution.days(ctx, s.workspaceID)
	if err != nil {
		return nil, err
	}
	var deals []wonDeal
	if err := r.db.WithContext(ctx).Raw(cte+`
SELECT o.id, o.lead_id, o.close_date AS won_at, o.value_cents
FROM opportunities o
WHERE o.workspace_id = ? AND o.status = ? AND o.deleted_at IS NULL AND o.close_date IS NOT NULL
	AND o.lead_id IN (SELECT s.lead_id FROM scope s WHERE s.message_id IS NOT NULL)`,
		append(args, s.workspaceID, opportunity.StatusWon)...).Scan(&deals).Error; err != nil {
		return nil, err
	}
	wins, err := r.attribution.lastTouches(ctx, s.workspaceID, deals, days)
	if err != nil {
		return nil, err
	}
	for _, w := range wins {
		if !s.holds(w.touch) {
			continue
		}
		cell := funnelCell{w.touch.CampaignID, w.touch.Variant}
		f := out[cell]
		f.Conversions++
		f.ConversionValueMicros += w.deal.ValueCents * centsToMicros
		out[cell] = f
	}
	return out, nil
}

func (r *FunnelReader) Funnel(ctx context.Context, q carriercampaign.FunnelQuery) (carriercampaign.Funnel, error) {
	cells, err := r.cells(ctx, scopeOf(q))
	if err != nil {
		return carriercampaign.Funnel{}, err
	}
	var total carriercampaign.Funnel
	for _, f := range cells {
		total = total.Add(f)
	}
	return total, nil
}

func (r *FunnelReader) VariantFunnels(ctx context.Context, q carriercampaign.FunnelQuery) (map[int]carriercampaign.Funnel, error) {
	cells, err := r.cells(ctx, scopeOf(q))
	if err != nil {
		return nil, err
	}
	out := map[int]carriercampaign.Funnel{}
	for cell, f := range cells {
		out[cell.variant] = out[cell.variant].Add(f)
	}
	return out, nil
}

func (r *FunnelReader) FunnelsOf(ctx context.Context, workspaceID string, campaignIDs []string) (map[string]carriercampaign.Funnel, error) {
	out := map[string]carriercampaign.Funnel{}
	if len(campaignIDs) == 0 {
		return out, nil
	}
	cells, err := r.cells(ctx, funnelScope{workspaceID: workspaceID, campaignIDs: campaignIDs})
	if err != nil {
		return nil, err
	}
	for cell, f := range cells {
		out[cell.campaignID] = out[cell.campaignID].Add(f)
	}
	return out, nil
}

func (r *FunnelReader) Touches(ctx context.Context, workspaceID, leadID string, since time.Time) ([]carriercampaign.Touch, error) {
	rows, err := r.attribution.touches(ctx, workspaceID, []string{leadID}, since)
	if err != nil {
		return nil, err
	}
	out := make([]carriercampaign.Touch, 0, len(rows))
	for _, row := range rows {
		if row.Channel == whatsAppChannel {
			continue
		}
		out = append(out, row.Touch)
	}
	return out, nil
}

func (r *FunnelReader) ButtonCounts(ctx context.Context, q carriercampaign.FunnelQuery, variant int) (map[rcs.ButtonKey]int64, map[rcs.ButtonKey]int64, error) {
	cte, args, err := scopeOf(q).cte()
	if err != nil {
		return nil, nil, err
	}
	var rows []struct {
		Kind        string
		ButtonCard  int
		ButtonIndex int
		Total       int64
	}
	if err := r.db.WithContext(ctx).Raw(cte+`
SELECT e.kind, e.button_card, e.button_index, COUNT(DISTINCT e.message_id) AS total
FROM tops t JOIN carrier_message_events e ON e.message_id = t.id
WHERE t.variant = ? AND e.kind IN ? AND e.button_index IS NOT NULL
GROUP BY 1, 2, 3`,
		append(args, variant, []carriermessaging.EventKind{carriermessaging.EventTap, carriermessaging.EventClick})...).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	taps, clicks := map[rcs.ButtonKey]int64{}, map[rcs.ButtonKey]int64{}
	for _, row := range rows {
		key := rcs.ButtonKey{Card: row.ButtonCard, Index: row.ButtonIndex}
		switch carriermessaging.EventKind(row.Kind) {
		case carriermessaging.EventTap:
			taps[key] += row.Total
		case carriermessaging.EventClick:
			clicks[key] += row.Total
		default:
			return nil, nil, fmt.Errorf("%w: button event kind %q", errUnknownStoredValue, row.Kind)
		}
	}
	return taps, clicks, nil
}

type carrierRow struct {
	Operator  string `gorm:"column:operator"`
	Sent      int64  `gorm:"column:sent"`
	Delivered int64  `gorm:"column:delivered"`
	FellBack  int64  `gorm:"column:fell_back"`
}

func toCarrierStats(rows []carrierRow) []carriercampaign.CarrierStat {
	out := make([]carriercampaign.CarrierStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, carriercampaign.CarrierStat{Operator: row.Operator, Sent: row.Sent, Delivered: row.Delivered, FellBack: row.FellBack})
	}
	return out
}

const carrierColumnsSQL = `
SELECT COALESCE(t.operator, '') AS operator, COUNT(*) AS sent,
	COUNT(*) FILTER (WHERE t.delivered_at IS NOT NULL) AS delivered,
	COUNT(*) FILTER (WHERE t.fallback_message_id IS NOT NULL) AS fell_back`

func (r *FunnelReader) Carriers(ctx context.Context, q carriercampaign.FunnelQuery) ([]carriercampaign.CarrierStat, error) {
	cte, args, err := scopeOf(q).cte()
	if err != nil {
		return nil, err
	}
	var rows []carrierRow
	if err := r.db.WithContext(ctx).Raw(cte+carrierColumnsSQL+`
FROM tops t
GROUP BY 1 ORDER BY sent DESC, operator ASC`, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return toCarrierStats(rows), nil
}
