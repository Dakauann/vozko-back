package analytics_repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
)

const uuidReference = `'^(refund:)?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`

type invoiceAccountRow struct {
	WABAID          string `gorm:"column:waba_id"`
	Name            string `gorm:"column:name"`
	Provider        string `gorm:"column:provider"`
	AccessToken     string `gorm:"column:access_token"`
	TemplateSends   int64  `gorm:"column:template_sends"`
	ServiceMessages int64  `gorm:"column:service_messages"`
}

var invoiceAccountsQuery = `
	WITH refs AS (
		SELECT bt.type, bt.is_refund,
		       regexp_replace(bt.reference_id, '^refund:', '')::uuid AS entry_id
		FROM balance_transactions bt
		WHERE bt.service_type = ` + campaignService + `
		  AND bt.created_at >= ? AND bt.created_at < ?
		  AND bt.reference_id ~* ` + uuidReference + `
	), tmpl AS (
		SELECT p.waba_id,
		       COALESCE(SUM(CASE WHEN refs.type = 'debit' AND refs.is_refund = false THEN 1
		                         WHEN refs.type = 'credit' AND refs.is_refund = true THEN -1
		                         ELSE 0 END), 0) AS template_sends
		FROM refs
		JOIN whatsapp_campaign_entries wce ON wce.id = refs.entry_id
		JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id
		JOIN whatsapp_business_phone_numbers p ON p.id = wc.business_phone_id
		GROUP BY p.waba_id
	), svc AS (
		SELECT p.waba_id, COUNT(*) AS service_messages
		FROM conversation_messages cm
		JOIN whatsapp_campaign_entries wce ON wce.id = cm.entry_id AND wce.deleted_at IS NULL
		JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.deleted_at IS NULL
		JOIN whatsapp_business_phone_numbers p ON p.id = wc.business_phone_id
		WHERE ` + serviceMessagePredicate + `
		  AND cm.created_at >= ? AND cm.created_at < ?
		GROUP BY p.waba_id
	)
	SELECT a.meta_waba_id AS waba_id,
	       COALESCE(a.name, '') AS name,
	       a.provider,
	       COALESCE(a.access_token, '') AS access_token,
	       COALESCE(tmpl.template_sends, 0) AS template_sends,
	       COALESCE(svc.service_messages, 0) AS service_messages
	FROM whatsapp_business_accounts a
	LEFT JOIN tmpl ON tmpl.waba_id = a.meta_waba_id
	LEFT JOIN svc ON svc.waba_id = a.meta_waba_id
	WHERE a.deleted_at IS NULL
	ORDER BY a.name, a.meta_waba_id`

func (r *repository) InvoiceAccounts(start, end time.Time) ([]analytics_domain.InvoiceAccount, error) {
	var rows []invoiceAccountRow
	if err := r.boundedRead(func(tx *gorm.DB) error {
		return tx.Raw(invoiceAccountsQuery, start, end, start, end).Scan(&rows).Error
	}); err != nil {
		return nil, fmt.Errorf("analytics invoice accounts: %w", err)
	}
	accounts := make([]analytics_domain.InvoiceAccount, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, analytics_domain.InvoiceAccount{
			WABAID:          row.WABAID,
			Name:            row.Name,
			Provider:        row.Provider,
			AccessToken:     row.AccessToken,
			TemplateSends:   row.TemplateSends,
			ServiceMessages: row.ServiceMessages,
		})
	}
	return accounts, nil
}
