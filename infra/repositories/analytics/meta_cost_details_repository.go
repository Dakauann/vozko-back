package analytics_repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
)

const metaCostDetailLimit = 200

type metaCostNumberRow struct {
	PhoneID            string     `gorm:"column:phone_id"`
	DisplayPhoneNumber string     `gorm:"column:display_phone_number"`
	Provider           string     `gorm:"column:provider"`
	WorkspaceName      string     `gorm:"column:workspace_name"`
	ServiceMessages    int64      `gorm:"column:service_messages"`
	Answered           int64      `gorm:"column:answered"`
	Charged            int64      `gorm:"column:charged"`
	FirstChargedAt     *time.Time `gorm:"column:first_charged_at"`
}

func (r *repository) MetaCostNumbers(input analytics_domain.MetaServiceMessageCostInput) ([]*analytics_domain.NumberMetaCost, error) {
	if input.Provider == analytics_domain.ServiceMessageProviderUnattributed {
		return []*analytics_domain.NumberMetaCost{}, nil
	}
	providerMatch, providerNeedsArg := providerFilterClause(input.Provider)
	args := make([]interface{}, 0, 4)
	if providerNeedsArg {
		args = append(args, string(input.Provider))
	}
	args = append(args, input.StartDate, input.EndDate, metaCostDetailLimit)

	query := `
		SELECT
			p.id AS phone_id,
			p.display_phone_number,
			p.provider,
			COALESCE(w.name, '') AS workspace_name,
			COUNT(*) AS service_messages,
			COUNT(*) FILTER (WHERE cm.meta_pricing_billable IS NOT NULL) AS answered,
			COUNT(*) FILTER (WHERE ` + metaConfirmedPredicate + `) AS charged,
			MIN(cm.created_at) FILTER (WHERE ` + metaConfirmedPredicate + `) AS first_charged_at
		FROM conversation_messages cm
		JOIN whatsapp_campaign_entries wce ON wce.id = cm.entry_id AND wce.deleted_at IS NULL
		JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.deleted_at IS NULL
		JOIN whatsapp_business_phone_numbers p ON p.id = wc.business_phone_id
		LEFT JOIN workspaces w ON w.id = p.owner_workspace_id
		WHERE ` + providerMatch + `
		  AND ` + serviceMessagePredicate + `
		  AND cm.created_at >= ? AND cm.created_at < ?
		GROUP BY p.id, p.display_phone_number, p.provider, w.name
		ORDER BY charged DESC, service_messages DESC
		LIMIT ?`

	var rows []metaCostNumberRow
	if err := r.boundedRead(func(tx *gorm.DB) error {
		return tx.Raw(query, args...).Scan(&rows).Error
	}); err != nil {
		return nil, fmt.Errorf("analytics meta cost per number: %w", err)
	}
	numbers := make([]*analytics_domain.NumberMetaCost, 0, len(rows))
	for _, row := range rows {
		numbers = append(numbers, &analytics_domain.NumberMetaCost{
			PhoneID:            row.PhoneID,
			DisplayPhoneNumber: row.DisplayPhoneNumber,
			Provider:           row.Provider,
			WorkspaceName:      row.WorkspaceName,
			ServiceMessages:    row.ServiceMessages,
			Answered:           row.Answered,
			Charged:            row.Charged,
			FirstChargedAt:     row.FirstChargedAt,
		})
	}
	return numbers, nil
}

type unlinkedNumberRow struct {
	PhoneNumberID      string `gorm:"column:phone_number_id"`
	DisplayPhoneNumber string `gorm:"column:display_phone_number"`
	Messages           int64  `gorm:"column:messages"`
}

func (r *repository) UnlinkedServiceMessages(input analytics_domain.MetaServiceMessageCostInput) ([]*analytics_domain.UnlinkedNumber, error) {
	var rows []unlinkedNumberRow
	err := r.db.Raw(`
		SELECT u.phone_number_id, COALESCE(p.display_phone_number, '') AS display_phone_number, COUNT(*) AS messages
		FROM whatsapp_unattributed_service_messages u
		LEFT JOIN whatsapp_business_phone_numbers p ON p.meta_phone_number_id = u.phone_number_id AND p.deleted_at IS NULL
		WHERE u.first_seen_at >= ? AND u.first_seen_at < ?
		GROUP BY u.phone_number_id, p.display_phone_number
		ORDER BY messages DESC
		LIMIT ?`, input.StartDate, input.EndDate, metaCostDetailLimit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("analytics unlinked service messages: %w", err)
	}
	unlinked := make([]*analytics_domain.UnlinkedNumber, 0, len(rows))
	for _, row := range rows {
		unlinked = append(unlinked, &analytics_domain.UnlinkedNumber{
			PhoneNumberID:      row.PhoneNumberID,
			DisplayPhoneNumber: row.DisplayPhoneNumber,
			Messages:           row.Messages,
		})
	}
	return unlinked, nil
}
