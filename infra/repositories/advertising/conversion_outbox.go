package advertising_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type conversionOutbox struct {
	db *gorm.DB
}

func NewConversionOutbox(db *gorm.DB) advertising.ConversionOutbox {
	return &conversionOutbox{db: db}
}

const pendingSignalsSQL = `WITH events AS (
	SELECT DISTINCT ON (ev.opportunity_id, ev.type) ev.opportunity_id, ev.type, ev.created_at
	FROM opportunity_events ev
	WHERE ev.workspace_id = ? AND ev.type IN (?, ?) AND ev.created_at >= ?
	ORDER BY ev.opportunity_id, ev.type, ev.created_at
),
signals AS (
	SELECT e.opportunity_id, e.created_at AS at,
		CASE e.type WHEN ? THEN ? WHEN ? THEN ? END AS event,
		CASE e.type WHEN ? THEN ? WHEN ? THEN ? END AS event_name,
		o.value_cents, o.currency, o.lead_id
	FROM events e
	JOIN opportunities o ON o.id = e.opportunity_id AND o.workspace_id = ? AND o.deleted_at IS NULL
)
SELECT s.opportunity_id, s.event, s.at, s.value_cents, COALESCE(s.currency, '') AS currency,
	CASE WHEN wc.id IS NOT NULL THEN ? WHEN fbc.id IS NOT NULL THEN ? WHEN igc.id IS NOT NULL THEN ? ELSE '' END AS channel,
	CASE WHEN wc.id IS NOT NULL THEN COALESCE(cao.click_id, '') ELSE '' END AS click_id,
	COALESCE(wbp.waba_id, '') AS waba_id,
	COALESCE(fbp.fb_page_id, '') AS page_id,
	COALESCE(fbct.psid, '') AS page_scoped_user_id,
	COALESCE(iga.ig_user_id, '') AS instagram_user_id,
	COALESCE(igct.igsid, '') AS instagram_scoped,
	COALESCE(l.number, '') AS phone
FROM signals s
LEFT JOIN LATERAL (
	SELECT oc.entry_id, oc.entry_type FROM opportunity_conversations oc
	WHERE oc.opportunity_id = s.opportunity_id
	ORDER BY oc.created_at, oc.id
	LIMIT 1
) link ON TRUE
LEFT JOIN conversation_ad_origins cao ON cao.entry_id = link.entry_id AND cao.entry_type = link.entry_type
LEFT JOIN whatsapp_campaign_entries wce ON link.entry_type = ? AND wce.id = link.entry_id
LEFT JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ?
LEFT JOIN whatsapp_business_phone_numbers wbp ON wc.id IS NOT NULL AND wbp.id = COALESCE(wce.received_business_phone_id, wc.business_phone_id)
LEFT JOIN facebook_conversations fbc ON link.entry_type = ? AND fbc.id = link.entry_id AND fbc.workspace_id = ?
LEFT JOIN facebook_contacts fbct ON fbct.id = fbc.contact_id
LEFT JOIN facebook_pages fbp ON fbp.id = fbc.page_id
LEFT JOIN instagram_conversations igc ON link.entry_type = ? AND igc.id = link.entry_id AND igc.workspace_id = ?
LEFT JOIN instagram_contacts igct ON igct.id = igc.contact_id
LEFT JOIN instagram_accounts iga ON iga.id = igc.ig_account_id
LEFT JOIN leads l ON l.id = s.lead_id AND l.workspace_id = ? AND l.deleted_at IS NULL
WHERE NOT EXISTS (
	SELECT 1 FROM ad_conversion_records r
	WHERE r.opportunity_id = s.opportunity_id AND r.event_name = s.event_name AND r.workspace_id = ?
	AND (r.status IN (?, ?, ?) OR (r.status = ? AND r.attempts >= ?))
)
ORDER BY s.at, s.opportunity_id, s.event
LIMIT ?`

func (o *conversionOutbox) Pending(ctx context.Context, workspaceID string, since time.Time, limit int) ([]advertising.PendingSignal, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	created, won := string(opportunity.EventCreated), string(opportunity.EventWon)
	args := []any{
		workspaceID, created, won, since,
		created, string(advertising.DealCreated), won, string(advertising.DealWon),
		created, advertising.EventNameLead, won, advertising.EventNamePurchase,
		workspaceID,
		string(advertising.ChannelWhatsApp), string(advertising.ChannelMessenger), string(advertising.ChannelInstagram),
		string(shared.EntryTypeWhatsApp), workspaceID,
		string(shared.EntryTypeFacebook), workspaceID,
		string(shared.EntryTypeInstagram), workspaceID,
		workspaceID,
		workspaceID, string(advertising.ConversionSent), string(advertising.ConversionSkipped), string(advertising.ConversionSending),
		string(advertising.ConversionFailed), exhaustedAttempts(),
		limit,
	}
	type pendingRow struct {
		OpportunityID    string    `gorm:"column:opportunity_id"`
		Event            string    `gorm:"column:event"`
		At               time.Time `gorm:"column:at"`
		ValueCents       int64     `gorm:"column:value_cents"`
		Currency         string    `gorm:"column:currency"`
		Channel          string    `gorm:"column:channel"`
		ClickID          string    `gorm:"column:click_id"`
		WABAID           string    `gorm:"column:waba_id"`
		PageID           string    `gorm:"column:page_id"`
		PageScopedUserID string    `gorm:"column:page_scoped_user_id"`
		InstagramUserID  string    `gorm:"column:instagram_user_id"`
		InstagramScoped  string    `gorm:"column:instagram_scoped"`
		Phone            string    `gorm:"column:phone"`
	}
	var rows []pendingRow
	if err := o.db.WithContext(ctx).Raw(pendingSignalsSQL, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]advertising.PendingSignal, 0, len(rows))
	for _, row := range rows {
		out = append(out, advertising.PendingSignal{
			WorkspaceID: workspaceID,
			Signal: advertising.DealSignal{
				OpportunityID: row.OpportunityID,
				Event:         advertising.DealEvent(row.Event),
				At:            row.At,
				ValueCents:    row.ValueCents,
				Currency:      row.Currency,
				Identity: advertising.MessagingIdentity{
					Channel:          advertising.MessagingChannel(row.Channel),
					ClickID:          row.ClickID,
					WABAID:           row.WABAID,
					PageID:           row.PageID,
					PageScopedUserID: row.PageScopedUserID,
					InstagramUserID:  row.InstagramUserID,
					InstagramScoped:  row.InstagramScoped,
				},
				Phone: row.Phone,
			},
		})
	}
	return out, nil
}

func exhaustedAttempts() int {
	attempts := 0
	for (advertising.ConversionRecord{Status: advertising.ConversionFailed, Attempts: attempts}).Retryable() {
		attempts++
	}
	return attempts
}

const recordConversionSQL = `INSERT INTO ad_conversion_records (opportunity_id, event_name, workspace_id, status, reason, attempts, sent_at, updated_at)
VALUES (?, ?, ?, ?, ?, CASE WHEN CAST(? AS text) = CAST(? AS text) THEN 1 ELSE 0 END, ?, NOW())
ON CONFLICT (opportunity_id, event_name) DO UPDATE SET
	status = EXCLUDED.status,
	reason = EXCLUDED.reason,
	attempts = ad_conversion_records.attempts + EXCLUDED.attempts,
	sent_at = EXCLUDED.sent_at,
	updated_at = NOW()
WHERE ad_conversion_records.workspace_id = EXCLUDED.workspace_id`

func (o *conversionOutbox) Record(ctx context.Context, r advertising.ConversionRecord) error {
	if blank(r.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(r.OpportunityID) || blank(r.EventName) {
		return errConversionKeyRequired
	}
	result := o.db.WithContext(ctx).Exec(recordConversionSQL,
		r.OpportunityID, r.EventName, r.WorkspaceID, string(r.Status), r.Reason,
		string(r.Status), string(advertising.ConversionFailed), r.SentAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errConversionRecordElsewhere
	}
	return nil
}

func (o *conversionOutbox) Recent(ctx context.Context, workspaceID string, limit int) ([]advertising.ConversionRecord, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdConversionRecord
	if err := o.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).
		Order("updated_at DESC, opportunity_id, event_name").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]advertising.ConversionRecord, 0, len(records))
	for _, record := range records {
		out = append(out, advertising.ConversionRecord{
			OpportunityID: record.OpportunityID,
			EventName:     record.EventName,
			WorkspaceID:   record.WorkspaceID,
			Status:        advertising.ConversionStatus(record.Status),
			Reason:        record.Reason,
			Attempts:      record.Attempts,
			SentAt:        record.SentAt,
			UpdatedAt:     record.UpdatedAt,
		})
	}
	return out, nil
}
