package analytics_repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database"
)

type metaServiceMessageCostRow struct {
	WorkspaceID      string  `gorm:"column:workspace_id"`
	WorkspaceName    string  `gorm:"column:workspace_name"`
	Providers        string  `gorm:"column:providers"`
	ServiceMessages  int64   `gorm:"column:service_messages"`
	MetaConfirmed    int64   `gorm:"column:meta_confirmed"`
	MetaAnswered     int64   `gorm:"column:meta_answered"`
	NetBillableSends int64   `gorm:"column:net_billable_sends"`
	TotalItems       int64   `gorm:"column:total_items"`
	TotalServiceMsgs int64   `gorm:"column:total_service_messages"`
	TotalMetaConfirm int64   `gorm:"column:total_meta_confirmed"`
	TotalMetaAnswer  int64   `gorm:"column:total_meta_answered"`
	TotalNetSends    int64   `gorm:"column:total_net_billable_sends"`
	TotalUnattrib    int64   `gorm:"column:total_unattributed"`
	RatioSort        float64 `gorm:"column:ratio_sort"`
}

const providerExpr = `COALESCE(p.provider, '` + string(analytics_domain.ServiceMessageProviderUnattributed) + `')`

var serviceMessagePredicate = database.ServiceMessagePredicateSQL("cm")

var metaConfirmedPredicate = `cm.meta_pricing_billable IS TRUE
			      AND cm.meta_pricing_category = ` + database.SQLStringLiteral(conversation.MetaPricingCategoryService) + `
			      AND COALESCE(cm.meta_conversation_origin, '') <> ` + database.SQLStringLiteral(conversation.MetaOriginFreeEntryPoint)

func metaCostOrderExpr(field analytics_domain.MetaServiceMessageCostSortField) string {
	switch field {
	case analytics_domain.SortMetaServiceMessageCostServiceMessages:
		return "service_messages"
	case analytics_domain.SortMetaServiceMessageCostNetBillableSends:
		return "net_billable_sends"
	case analytics_domain.SortMetaServiceMessageCostWorkspaceName:
		return "workspace_name"
	default:
		return "ratio_sort"
	}
}

func metaCostOrderDirection(direction shared.SortDirection) string {
	if direction == shared.SortAsc {
		return "ASC"
	}
	return "DESC"
}

func providerFilterClause(provider analytics_domain.ServiceMessageProvider) (clause string, needsArg bool) {
	switch provider {
	case analytics_domain.ServiceMessageProviderAll:
		return "TRUE", false
	case analytics_domain.ServiceMessageProviderUnattributed:
		return "p.id IS NULL", false
	default:
		return providerExpr + " = ?", true
	}
}

func (r *repository) GetMetaServiceMessageCost(input analytics_domain.MetaServiceMessageCostInput) (*analytics_domain.MetaServiceMessageCostReport, error) {
	pagination := shared.NormalizePagination(shared.Pagination{Page: input.Page, PageSize: input.PageSize})

	providerMatch, providerNeedsArg := providerFilterClause(input.Provider)
	orderExpr := metaCostOrderExpr(input.SortBy)
	orderDirection := metaCostOrderDirection(input.SortOrder)

	args := make([]interface{}, 0, 8)

	query := `
		WITH svc AS (
			SELECT
				wc.workspace_id AS workspace_id,
				COUNT(*) FILTER (WHERE ` + providerMatch + `) AS service_messages,
				COUNT(*) FILTER (WHERE ` + providerMatch + ` AND ` + metaConfirmedPredicate + `) AS meta_confirmed,
				COUNT(*) FILTER (WHERE ` + providerMatch + ` AND cm.meta_pricing_billable IS NOT NULL) AS meta_answered,
				COUNT(*) FILTER (WHERE p.id IS NULL) AS unattributed_messages,
				COALESCE(STRING_AGG(DISTINCT ` + providerExpr + `, ',') FILTER (WHERE ` + providerMatch + `), '') AS providers
			FROM conversation_messages cm
			JOIN whatsapp_campaign_entries wce ON wce.id = cm.entry_id AND wce.deleted_at IS NULL
			JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.deleted_at IS NULL
			LEFT JOIN whatsapp_business_phone_numbers p ON p.id = wc.business_phone_id
			WHERE ` + serviceMessagePredicate + `
			  AND cm.created_at >= ? AND cm.created_at < ?
			GROUP BY wc.workspace_id
		), bill AS (
			SELECT
				workspace_id,
				COALESCE(SUM(CASE WHEN type = 'debit'  AND is_refund = false THEN 1
				                  WHEN type = 'credit' AND is_refund = true  THEN -1
				                  ELSE 0 END), 0) AS net_billable_sends
			FROM balance_transactions
			WHERE service_type = ?
			  AND created_at >= ? AND created_at < ?
			GROUP BY workspace_id
		), unattributed AS (
			SELECT COALESCE(SUM(unattributed_messages), 0) AS total FROM svc
		), exposure_rows AS (
			SELECT
				w.id AS workspace_id,
				w.name AS workspace_name,
				COALESCE(svc.providers, '') AS providers,
				COALESCE(svc.meta_confirmed, 0) AS meta_confirmed,
				COALESCE(svc.meta_answered, 0) AS meta_answered,
				COALESCE(svc.service_messages, 0) AS service_messages,
				COALESCE(bill.net_billable_sends, 0) AS net_billable_sends
			FROM workspaces w
			LEFT JOIN svc  ON svc.workspace_id  = w.id
			LEFT JOIN bill ON bill.workspace_id = w.id
			WHERE w.deleted_at IS NULL
			  AND (COALESCE(svc.service_messages, 0) > 0 OR COALESCE(bill.net_billable_sends, 0) <> 0)`

	if providerNeedsArg {
		provider := string(input.Provider)
		args = append(args, provider, provider, provider, provider)
	}
	args = append(args,
		input.StartDate, input.EndDate,
		string(balance.ServiceWhatsAppCampaign),
		input.StartDate, input.EndDate,
	)

	if search := strings.TrimSpace(input.Search); search != "" {
		query += `
			  AND w.name ILIKE ?`
		args = append(args, "%"+search+"%")
	}

	query += `
		)
		SELECT
			workspace_id,
			workspace_name,
			providers,
			service_messages,
			meta_confirmed,
			meta_answered,
			net_billable_sends,
			CASE
				WHEN net_billable_sends > 0 THEN service_messages::double precision / net_billable_sends
				WHEN service_messages   > 0 THEN 'Infinity'::double precision
				ELSE 0
			END AS ratio_sort,
			COUNT(*) OVER () AS total_items,
			SUM(service_messages) OVER () AS total_service_messages,
			SUM(meta_confirmed) OVER () AS total_meta_confirmed,
			SUM(meta_answered) OVER () AS total_meta_answered,
			SUM(net_billable_sends) OVER () AS total_net_billable_sends,
			(SELECT total FROM unattributed) AS total_unattributed
		FROM exposure_rows
	`
	query += fmt.Sprintf(` ORDER BY %s %s, workspace_name ASC LIMIT ? OFFSET ?`, orderExpr, orderDirection)
	args = append(args, pagination.PageSize, pagination.Offset())

	var rows []metaServiceMessageCostRow
	if err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL jit = off").Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL statement_timeout = '15s'").Error; err != nil {
			return err
		}
		return tx.Raw(query, args...).Scan(&rows).Error
	}); err != nil {
		return nil, fmt.Errorf("analytics service exposure: %w", err)
	}

	return buildMetaServiceMessageCost(input, pagination, rows), nil
}

func buildMetaServiceMessageCost(
	input analytics_domain.MetaServiceMessageCostInput,
	pagination shared.Pagination,
	rows []metaServiceMessageCostRow,
) *analytics_domain.MetaServiceMessageCostReport {
	items := make([]*analytics_domain.WorkspaceMetaServiceMessageCost, 0, len(rows))
	for _, row := range rows {
		items = append(items, &analytics_domain.WorkspaceMetaServiceMessageCost{
			WorkspaceID:      row.WorkspaceID,
			WorkspaceName:    row.WorkspaceName,
			Providers:        splitProviders(row.Providers),
			ServiceMessages:  row.ServiceMessages,
			MetaConfirmed:    row.MetaConfirmed,
			NetBillableSends: row.NetBillableSends,
			Ratio:            analytics_domain.ComputeRatio(row.ServiceMessages, row.NetBillableSends),
		})
	}

	totals := analytics_domain.MetaServiceMessageCostTotals{}
	if len(rows) > 0 {
		totals.ServiceMessages = rows[0].TotalServiceMsgs
		totals.MetaConfirmed = rows[0].TotalMetaConfirm
		totals.MetaAnswered = rows[0].TotalMetaAnswer
		totals.NetBillableSends = rows[0].TotalNetSends
		totals.WorkspacesCovered = rows[0].TotalItems
		totals.UnattributedServiceMessages = rows[0].TotalUnattrib
	}
	totals.Ratio = analytics_domain.ComputeRatio(totals.ServiceMessages, totals.NetBillableSends)

	return &analytics_domain.MetaServiceMessageCostReport{
		InferredOnly: !totals.FullyAnsweredByMeta(),
		Period: analytics_domain.Period{
			StartDate: input.StartDate,
			EndDate:   input.EndDate,
		},
		Provider:   input.Provider,
		Totals:     totals,
		Workspaces: shared.NewPaginatedResult(items, pagination, totals.WorkspacesCovered),
	}
}

func splitProviders(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Split(trimmed, ",")
	providers := make([]string, 0, len(parts))
	for _, part := range parts {
		if p := strings.TrimSpace(part); p != "" {
			providers = append(providers, p)
		}
	}
	return providers
}
