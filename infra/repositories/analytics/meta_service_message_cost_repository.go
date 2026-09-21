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

// The service message side of this report cannot come from the ledger: nothing
// in the tree writes a balance transaction for a session message, so the count
// has to be taken from conversation_messages. The billable send side does come
// from the ledger, because that is where the money actually moved.
//
// Both sides are aggregated per workspace over the period and joined once. The
// page, the period totals and the unattributed bucket all come out of a single
// round trip: totals are window functions over the already-narrowed row set, so
// asking for them costs a sort of a few hundred rows rather than a second pass
// over three million messages.

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

// providerExpr names the provider a counted message belongs to. A campaign with
// no business phone cannot name one, and those messages must stay visible under
// their own label rather than vanish into a NULL that every comparison rejects.
const providerExpr = `COALESCE(p.provider, '` + string(analytics_domain.ServiceMessageProviderUnattributed) + `')`

// serviceMessagePredicate selects the messages Meta will charge us for, with
// the domain's constants INLINED rather than bound.
//
// Inlining is not a shortcut, it is the whole point. idx_cm_service_exposure is
// a partial index, and Postgres only uses a partial index when it can prove the
// query's WHERE implies the index's predicate. It cannot prove that about a
// bind parameter under a generic plan, and a prepared statement reaches a
// generic plan after a handful of executions. Bound, this query would quietly
// fall back to a full scan of three million rows once the page had been opened
// six times, which is the worst possible failure: correct, and slow only in
// production.
//
// Nothing here is request data. Every value is a compile-time constant from
// domain/conversation, rendered through the same helper that builds the index
// predicate, so the two cannot drift apart.
var serviceMessagePredicate = database.ServiceMessagePredicateSQL("cm")

// metaConfirmedPredicate selects the messages Meta itself stamped billable as
// service, rather than the ones our own rule infers.
//
// It is a FILTER inside the same aggregate and not a second query, so the
// confirmed count costs nothing beyond the scan already being done. Rows
// delivered before the pricing columns existed carry NULL and are excluded,
// which is the point: "Meta has not told us" is not "Meta said no".
var metaConfirmedPredicate = `cm.meta_pricing_billable IS TRUE
			      AND cm.meta_pricing_category = ` + database.SQLStringLiteral(conversation.MetaPricingCategoryService) + `
			      AND COALESCE(cm.meta_conversation_origin, '') <> ` + database.SQLStringLiteral(conversation.MetaOriginFreeEntryPoint)

// metaCostOrderExpr maps a whitelisted sort field to a column.
//
// The usecase has already replaced anything not on the whitelist, and this
// switch has no default that could pass raw input through, so nothing
// attacker-controlled ever reaches the interpolation.
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

// providerFilterClause renders the provider narrowing as a boolean expression
// used inside a FILTER clause rather than in the WHERE.
//
// Keeping it out of the WHERE is what lets one scan answer two questions: how
// many messages match the chosen provider, and how many could not be attributed
// to any provider at all. It costs nothing, because the provider lives on the
// phone table and could never have narrowed the message scan anyway.
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

	// svc counts what Meta will charge us for. Its predicate is inlined from the
	// domain rather than bound, so the partial index can serve it; see
	// serviceMessagePredicate. Everything that genuinely varies per request, the
	// period, the provider, the search and the page, is still bound.
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
		// Four placeholders, in the order they appear in the SELECT above: the
		// count filter, the Meta-confirmed filter, the Meta-answered filter and
		// the provider list filter.
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

	// ratio_sort exists so "sent messages but bought nothing" sorts above every
	// finite ratio instead of below them as a NULL would. It is a sort key only:
	// the ratio the page prints stays nil in that case, because an infinity is
	// not a number an operator can act on.
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

	// Two guards, both LOCAL to this transaction so the pooled connection goes
	// back untouched for everything else.
	//
	// jit = off because this is index seeks and aggregation over rows that have
	// to be fetched either way, never CPU-bound expression evaluation over many
	// rows, which is the only thing JIT compilation pays for. The attendance
	// reports measured the same shape of query going from 6,92s to 2,42s purely
	// from turning it off.
	//
	// statement_timeout because an analytical query with no ceiling over these
	// exact tables took the platform down on 2026-09-08: nine concurrent copies
	// accumulated against a server running statement_timeout = 0, load hit 14,
	// and /auth/login stopped answering. This query is expected to finish in
	// well under a second once idx_cm_service_exposure exists, so fifteen
	// seconds is roughly twenty times the budget. A report that cannot finish in
	// fifteen seconds is a failed report, and a failed report is survivable in a
	// way that an unbounded one is not.
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

// buildMetaServiceMessageCost turns the flat rows into the report.
//
// The window columns repeat the same totals on every row, so they are read from
// the first row only. An empty page is not an error: it means the filters match
// nothing, and the totals are then genuinely zero.
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
		// Derived here, where the coverage is actually known, rather than
		// asserted by the usecase. Once Meta has answered for every message in
		// the period the figure stops being our inference and the page stops
		// calling it an upper bound, without anyone editing code.
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

// splitProviders turns the aggregated provider list back into a slice. Postgres
// STRING_AGG is used rather than array_agg so the result scans as a plain
// string on any driver, without a driver-specific array type.
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
