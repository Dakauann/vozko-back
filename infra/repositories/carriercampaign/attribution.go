package carriercampaign_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/opportunity"
)

const (
	centsToMicros   = 10_000
	whatsAppChannel = "whatsapp"
)

var touchingRunKinds = []carriermessaging.RunKind{carriermessaging.RunCampaign, carriermessaging.RunAutomation}

type wonDeal struct {
	ID         string
	LeadID     string
	WonAt      time.Time
	ValueCents int64
}

type touch struct {
	carriercampaign.Touch
	LeadID  string
	Channel string
}

const touchesSQL = `
SELECT m.id::text AS message_id, COALESCE(r.campaign_id::text, '') AS campaign_id, COALESCE(r.run_id::text, '') AS run_id,
	COALESCE(r.variant, 0) AS variant, COALESCE(m.lead_id, r.lead_id)::text AS lead_id, m.channel,
	LEAST(m.delivered_at, d.delivered_at) AS delivered_at
FROM carrier_messages m
LEFT JOIN carrier_campaign_recipients r ON r.message_id = m.id
LEFT JOIN LATERAL (
	SELECT MIN(x.delivered_at) AS delivered_at
	FROM carrier_messages c
	LEFT JOIN carrier_messages g ON g.parent_message_id = c.id
	CROSS JOIN LATERAL (VALUES (c.delivered_at), (g.delivered_at)) AS x(delivered_at)
	WHERE c.parent_message_id = m.id
) d ON true
WHERE m.workspace_id = ? AND m.parent_message_id IS NULL AND m.run_kind IN ?
	AND COALESCE(m.lead_id, r.lead_id) IN ? AND LEAST(m.delivered_at, d.delivered_at) >= ?
UNION ALL
SELECT e.id::text, e.campaign_id::text, '', 0, e.lead_id::text, '` + whatsAppChannel + `', e.delivered_at
FROM whatsapp_campaign_entries e
JOIN whatsapp_campaigns wc ON wc.id = e.campaign_id
WHERE wc.workspace_id = ? AND e.deleted_at IS NULL AND e.lead_id IN ? AND e.delivered_at >= ?
ORDER BY 7, 1`

const dealsWonBetweenSQL = `
SELECT o.id, o.lead_id, o.close_date AS won_at, o.value_cents
FROM opportunities o
WHERE o.workspace_id = ? AND o.status = ? AND o.deleted_at IS NULL AND o.lead_id IS NOT NULL
	AND o.close_date >= ? AND o.close_date < ?`

type attribution struct {
	db *gorm.DB
}

func (a attribution) touches(ctx context.Context, workspaceID string, leadIDs []string, since time.Time) ([]touch, error) {
	var rows []struct {
		MessageID   string
		CampaignID  string
		RunID       string
		Variant     int
		LeadID      string
		Channel     string
		DeliveredAt time.Time
	}
	if err := a.db.WithContext(ctx).Raw(touchesSQL,
		workspaceID, touchingRunKinds, leadIDs, since,
		workspaceID, leadIDs, since,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]touch, 0, len(rows))
	for _, row := range rows {
		out = append(out, touch{
			Touch:   carriercampaign.Touch{MessageID: row.MessageID, CampaignID: row.CampaignID, RunID: row.RunID, Variant: row.Variant, DeliveredAt: row.DeliveredAt},
			LeadID:  row.LeadID,
			Channel: row.Channel,
		})
	}
	return out, nil
}

func (a attribution) days(ctx context.Context, workspaceID string) (int, error) {
	var days []int
	if err := a.db.WithContext(ctx).Raw(`SELECT attribution_days FROM carrier_settings WHERE workspace_id = ?`, workspaceID).Scan(&days).Error; err != nil {
		return 0, err
	}
	if len(days) == 0 {
		return carriermessaging.DefaultAttributionDays, nil
	}
	return days[0], nil
}

func (a attribution) dealsWonBetween(ctx context.Context, workspaceID string, from, to time.Time) ([]wonDeal, error) {
	var deals []wonDeal
	err := a.db.WithContext(ctx).Raw(dealsWonBetweenSQL, workspaceID, opportunity.StatusWon, from, to).Scan(&deals).Error
	return deals, err
}

type attributed struct {
	deal  wonDeal
	touch touch
}

func (a attribution) lastTouches(ctx context.Context, workspaceID string, deals []wonDeal, days int) ([]attributed, error) {
	if len(deals) == 0 {
		return nil, nil
	}
	leads := make([]string, 0, len(deals))
	earliest := deals[0].WonAt
	for _, d := range deals {
		leads = append(leads, d.LeadID)
		if d.WonAt.Before(earliest) {
			earliest = d.WonAt
		}
	}
	touches, err := a.touches(ctx, workspaceID, leads, earliest.Add(-time.Duration(days)*24*time.Hour))
	if err != nil {
		return nil, err
	}
	byLead := map[string][]carriercampaign.Touch{}
	byMessage := map[string]touch{}
	for _, t := range touches {
		byLead[t.LeadID] = append(byLead[t.LeadID], t.Touch)
		byMessage[t.MessageID] = t
	}
	var out []attributed
	for _, d := range deals {
		winner, ok := carriercampaign.LastTouch(byLead[d.LeadID], d.WonAt, days)
		if !ok {
			continue
		}
		out = append(out, attributed{deal: d, touch: byMessage[winner.MessageID]})
	}
	return out, nil
}
