package analytics_repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/shared"
)

const (
	defaultAdminOverviewRecentLimit = 8
	maxAdminOverviewRecentLimit     = 25
)

const operationalServiceFilter = `service_type NOT IN ('top_up', 'manual_adjustment')`

type focusedWorkspaceRow struct {
	WorkspaceID           string       `gorm:"column:workspace_id"`
	WorkspaceName         string       `gorm:"column:workspace_name"`
	CurrentBalanceMicros  int64        `gorm:"column:current_balance_micros"`
	LifetimeCreditsMicros int64        `gorm:"column:lifetime_credits_micros"`
	LifetimeDebitsMicros  int64        `gorm:"column:lifetime_debits_micros"`
	PeriodCreditsMicros   int64        `gorm:"column:period_credits_micros"`
	PeriodDebitsMicros    int64        `gorm:"column:period_debits_micros"`
	PeriodTxCount         int64        `gorm:"column:period_tx_count"`
	LastTransactionAt     sql.NullTime `gorm:"column:last_transaction_at"`
}

type workspaceSnapshotRow struct {
	WorkspaceID          string       `gorm:"column:workspace_id"`
	WorkspaceName        string       `gorm:"column:workspace_name"`
	CurrentBalanceMicros int64        `gorm:"column:current_balance_micros"`
	PeriodCreditsMicros  int64        `gorm:"column:period_credits_micros"`
	RevenueMicros        int64        `gorm:"column:revenue_micros"`
	CostMicros           int64        `gorm:"column:cost_micros"`
	ProfitMicros         int64        `gorm:"column:profit_micros"`
	RevenueBRLMicros     int64        `gorm:"column:revenue_brl_micros"`
	CostBRLMicros        int64        `gorm:"column:cost_brl_micros"`
	ProfitBRLMicros      int64        `gorm:"column:profit_brl_micros"`
	RefundsMicros        int64        `gorm:"column:refunds_micros"`
	RefundsBRLMicros     int64        `gorm:"column:refunds_brl_micros"`
	TxCount              int64        `gorm:"column:tx_count"`
	LastTransactionAt    sql.NullTime `gorm:"column:last_transaction_at"`
}

type recentSpendingRow struct {
	TransactionID      string    `gorm:"column:transaction_id"`
	WorkspaceID        string    `gorm:"column:workspace_id"`
	WorkspaceName      string    `gorm:"column:workspace_name"`
	ServiceType        string    `gorm:"column:service_type"`
	Description        string    `gorm:"column:description"`
	AmountMicros       int64     `gorm:"column:amount_micros"`
	CostMicros         int64     `gorm:"column:cost_micros"`
	ProfitMicros       int64     `gorm:"column:profit_micros"`
	ExchangeRateMicros int64     `gorm:"column:exchange_rate_micros"`
	BalanceAfterMicros int64     `gorm:"column:balance_after_micros"`
	CreatedAt          time.Time `gorm:"column:created_at"`
}

type usageMetricRow struct {
	Key              string `gorm:"column:key"`
	Count            int64  `gorm:"column:count"`
	RevenueMicros    int64  `gorm:"column:revenue_micros"`
	RevenueBRLMicros int64  `gorm:"column:revenue_brl_micros"`
}

type categoryUsageRow struct {
	Key   string `gorm:"column:key"`
	Count int64  `gorm:"column:count"`
}

func (r *repository) GetAdminOverview(input analytics_domain.AdminOverviewInput) (*analytics_domain.AdminOverview, error) {
	pagination := shared.NormalizePagination(shared.Pagination{
		Page:     input.WorkspacePage,
		PageSize: input.WorkspacePageSize,
	})

	recentLimit := input.RecentLimit
	if recentLimit <= 0 {
		recentLimit = defaultAdminOverviewRecentLimit
	}
	if recentLimit > maxAdminOverviewRecentLimit {
		recentLimit = maxAdminOverviewRecentLimit
	}

	overview := &analytics_domain.AdminOverview{
		Period: analytics_domain.Period{
			StartDate: input.StartDate,
			EndDate:   input.EndDate,
		},
	}

	if input.WorkspaceID != nil && strings.TrimSpace(*input.WorkspaceID) != "" {
		focusedWorkspace, err := r.getFocusedWorkspaceBalance(strings.TrimSpace(*input.WorkspaceID), input.StartDate, input.EndDate)
		if err != nil {
			return nil, err
		}
		overview.FocusedWorkspace = focusedWorkspace
	}

	workspaceSnapshots, err := r.listWorkspaceFinancialSnapshots(
		input.WorkspaceSearch,
		input.WorkspaceSortBy,
		input.WorkspaceSortOrder,
		input.StartDate,
		input.EndDate,
		pagination,
	)
	if err != nil {
		return nil, err
	}
	overview.WorkspaceSnapshots = workspaceSnapshots

	recentSpendings, err := r.listRecentSpendings(input.WorkspaceID, input.StartDate, input.EndDate, recentLimit)
	if err != nil {
		return nil, err
	}
	overview.RecentSpendings = recentSpendings

	productUsage, err := r.getProductUsageSummary(input.WorkspaceID, input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	overview.ProductUsage = *productUsage

	var totalPeriodCredits int64
	var totalPeriodCreditsBRL int64
	var totalPeriodRefunds int64
	var totalPeriodRefundsBRL int64
	creditsQuery := `SELECT
			COALESCE(SUM(CASE WHEN type = 'credit' AND is_refund = false THEN amount
			                  WHEN type = 'debit' AND service_type = 'top_up' THEN -amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'credit' AND is_refund = false THEN amount * exchange_rate_micros / 1000000
			                  WHEN type = 'debit' AND service_type = 'top_up' THEN -amount * exchange_rate_micros / 1000000 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN is_refund = true AND ` + operationalServiceFilter + ` THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN is_refund = true AND ` + operationalServiceFilter + ` THEN amount * exchange_rate_micros / 1000000 ELSE 0 END), 0)
		FROM balance_transactions
		WHERE created_at >= ? AND created_at <= ?
		  AND (type = 'credit' OR (type = 'debit' AND service_type = 'top_up'))`
	creditsArgs := []interface{}{input.StartDate, input.EndDate}
	if input.WorkspaceID != nil && strings.TrimSpace(*input.WorkspaceID) != "" {
		creditsQuery += ` AND workspace_id = ?`
		creditsArgs = append(creditsArgs, strings.TrimSpace(*input.WorkspaceID))
	}
	if err := r.db.Raw(creditsQuery, creditsArgs...).Row().Scan(&totalPeriodCredits, &totalPeriodCreditsBRL, &totalPeriodRefunds, &totalPeriodRefundsBRL); err != nil {
		return nil, fmt.Errorf("analytics total period credits: %w", err)
	}
	overview.TotalPeriodCreditsMicros = totalPeriodCredits
	overview.TotalPeriodCreditsBRLMicros = totalPeriodCreditsBRL
	overview.TotalPeriodRefundsMicros = totalPeriodRefunds
	overview.TotalPeriodRefundsBRLMicros = totalPeriodRefundsBRL

	return overview, nil
}

func (r *repository) getFocusedWorkspaceBalance(workspaceID string, startDate, endDate time.Time) (*analytics_domain.FocusedWorkspaceBalance, error) {
	query := `
		SELECT
			w.id AS workspace_id,
			w.name AS workspace_name,
			COALESCE(b.amount, 0) AS current_balance_micros,
			COALESCE(SUM(CASE WHEN bt.type = 'credit' THEN bt.amount ELSE 0 END), 0) AS lifetime_credits_micros,
			COALESCE(SUM(CASE WHEN bt.type = 'debit' THEN bt.amount ELSE 0 END), 0) AS lifetime_debits_micros,
			COALESCE(SUM(CASE WHEN bt.type = 'credit' AND bt.created_at >= ? AND bt.created_at <= ? THEN bt.amount ELSE 0 END), 0) AS period_credits_micros,
			COALESCE(SUM(CASE WHEN bt.type = 'debit' AND bt.created_at >= ? AND bt.created_at <= ? THEN bt.amount ELSE 0 END), 0) AS period_debits_micros,
			COALESCE(COUNT(CASE WHEN bt.type = 'debit' AND bt.created_at >= ? AND bt.created_at <= ? THEN 1 END), 0) AS period_tx_count,
			MAX(bt.created_at) AS last_transaction_at
		FROM workspaces w
		LEFT JOIN balances b ON b.workspace_id = w.id
		LEFT JOIN balance_transactions bt ON bt.workspace_id = w.id
		WHERE w.deleted_at IS NULL AND w.id = ?
		GROUP BY w.id, w.name, b.amount
	`

	var row focusedWorkspaceRow
	result := r.db.Raw(query, startDate, endDate, startDate, endDate, startDate, endDate, workspaceID).Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("analytics focused workspace: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return &analytics_domain.FocusedWorkspaceBalance{
		WorkspaceID:           row.WorkspaceID,
		WorkspaceName:         row.WorkspaceName,
		CurrentBalanceMicros:  row.CurrentBalanceMicros,
		LifetimeCreditsMicros: row.LifetimeCreditsMicros,
		LifetimeDebitsMicros:  row.LifetimeDebitsMicros,
		PeriodCreditsMicros:   row.PeriodCreditsMicros,
		PeriodDebitsMicros:    row.PeriodDebitsMicros,
		PeriodTxCount:         row.PeriodTxCount,
		LastTransactionAt:     nullableTime(row.LastTransactionAt),
	}, nil
}

func (r *repository) listWorkspaceFinancialSnapshots(search, sortBy, sortOrder string, startDate, endDate time.Time, pagination shared.Pagination) (*shared.PaginatedResult[*analytics_domain.WorkspaceFinancialSnapshot], error) {
	countQuery := `SELECT COUNT(*) FROM workspaces w WHERE w.deleted_at IS NULL`
	countArgs := make([]interface{}, 0, 1)
	if trimmed := strings.TrimSpace(search); trimmed != "" {
		countQuery += ` AND w.name ILIKE ?`
		countArgs = append(countArgs, "%"+trimmed+"%")
	}

	var total int64
	if err := r.db.Raw(countQuery, countArgs...).Scan(&total).Error; err != nil {
		return nil, fmt.Errorf("analytics workspace snapshots count: %w", err)
	}

	orderField := normalizeOverviewSortField(sortBy)
	orderDirection := normalizeOverviewSortDirection(sortOrder)

	query := `
		SELECT
			w.id AS workspace_id,
			w.name AS workspace_name,
			COALESCE(b.amount, 0) AS current_balance_micros,
			COALESCE(period_tx.period_credits_micros, 0) AS period_credits_micros,
			COALESCE(period_tx.revenue_micros, 0) AS revenue_micros,
			COALESCE(period_tx.cost_micros, 0) AS cost_micros,
			COALESCE(period_tx.profit_micros, 0) AS profit_micros,
			COALESCE(period_tx.revenue_brl_micros, 0) AS revenue_brl_micros,
			COALESCE(period_tx.cost_brl_micros, 0) AS cost_brl_micros,
			COALESCE(period_tx.profit_brl_micros, 0) AS profit_brl_micros,
			COALESCE(period_tx.refunds_micros, 0) AS refunds_micros,
			COALESCE(period_tx.refunds_brl_micros, 0) AS refunds_brl_micros,
			COALESCE(period_tx.tx_count, 0) AS tx_count,
			period_tx.last_transaction_at AS last_transaction_at
		FROM workspaces w
		LEFT JOIN balances b ON b.workspace_id = w.id
		LEFT JOIN (
			SELECT
				workspace_id,
				COALESCE(SUM(CASE WHEN type = 'credit' AND is_refund = false THEN amount ELSE 0 END), 0) AS period_credits_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND type = 'debit' AND is_refund = false THEN amount
				                  WHEN ` + operationalServiceFilter + ` AND type = 'credit' AND is_refund = true THEN -amount ELSE 0 END), 0) AS revenue_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND type = 'debit' AND is_refund = false THEN cost_micros
				                  WHEN ` + operationalServiceFilter + ` AND type = 'credit' AND is_refund = true THEN -cost_micros ELSE 0 END), 0) AS cost_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND ((type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true)) THEN profit_micros ELSE 0 END), 0) AS profit_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND type = 'debit' AND is_refund = false THEN amount * exchange_rate_micros / 1000000
				                  WHEN ` + operationalServiceFilter + ` AND type = 'credit' AND is_refund = true THEN -amount * exchange_rate_micros / 1000000 ELSE 0 END), 0) AS revenue_brl_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND type = 'debit' AND is_refund = false THEN cost_micros * exchange_rate_micros / 1000000
				                  WHEN ` + operationalServiceFilter + ` AND type = 'credit' AND is_refund = true THEN -cost_micros * exchange_rate_micros / 1000000 ELSE 0 END), 0) AS cost_brl_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND ((type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true)) THEN profit_micros * exchange_rate_micros / 1000000 ELSE 0 END), 0) AS profit_brl_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND is_refund = true THEN amount ELSE 0 END), 0) AS refunds_micros,
				COALESCE(SUM(CASE WHEN ` + operationalServiceFilter + ` AND is_refund = true THEN amount * exchange_rate_micros / 1000000 ELSE 0 END), 0) AS refunds_brl_micros,
				COALESCE(COUNT(CASE WHEN type = 'debit' AND is_refund = false THEN 1 END), 0) AS tx_count,
				MAX(created_at) AS last_transaction_at
			FROM balance_transactions
			WHERE created_at >= ? AND created_at <= ?
			GROUP BY workspace_id
		) AS period_tx ON period_tx.workspace_id = w.id
		WHERE w.deleted_at IS NULL
	`

	args := []interface{}{startDate, endDate}
	if trimmed := strings.TrimSpace(search); trimmed != "" {
		query += ` AND w.name ILIKE ?`
		args = append(args, "%"+trimmed+"%")
	}

	query += fmt.Sprintf(` ORDER BY %s %s NULLS LAST, workspace_name ASC LIMIT ? OFFSET ?`, orderField, orderDirection)
	args = append(args, pagination.PageSize, pagination.Offset())

	var rows []workspaceSnapshotRow
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("analytics workspace snapshots: %w", err)
	}

	items := make([]*analytics_domain.WorkspaceFinancialSnapshot, 0, len(rows))
	for _, row := range rows {
		items = append(items, &analytics_domain.WorkspaceFinancialSnapshot{
			WorkspaceID:          row.WorkspaceID,
			WorkspaceName:        row.WorkspaceName,
			CurrentBalanceMicros: row.CurrentBalanceMicros,
			PeriodCreditsMicros:  row.PeriodCreditsMicros,
			RevenueMicros:        row.RevenueMicros,
			CostMicros:           row.CostMicros,
			ProfitMicros:         row.ProfitMicros,
			RevenueBRLMicros:     row.RevenueBRLMicros,
			CostBRLMicros:        row.CostBRLMicros,
			ProfitBRLMicros:      row.ProfitBRLMicros,
			RefundsMicros:        row.RefundsMicros,
			RefundsBRLMicros:     row.RefundsBRLMicros,
			MarginPct:            computeMargin(row.RevenueMicros, row.ProfitMicros),
			TxCount:              row.TxCount,
			LastTransactionAt:    nullableTime(row.LastTransactionAt),
		})
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) listRecentSpendings(workspaceID *string, startDate, endDate time.Time, limit int) ([]analytics_domain.RecentSpending, error) {
	query := `
		SELECT
			bt.id AS transaction_id,
			bt.workspace_id AS workspace_id,
			w.name AS workspace_name,
			bt.service_type AS service_type,
			bt.description AS description,
			bt.amount AS amount_micros,
			bt.cost_micros AS cost_micros,
			bt.profit_micros AS profit_micros,
			bt.exchange_rate_micros AS exchange_rate_micros,
			bt.balance_after AS balance_after_micros,
			bt.created_at AS created_at
		FROM balance_transactions bt
		JOIN workspaces w ON w.id = bt.workspace_id
		WHERE w.deleted_at IS NULL
		  AND bt.type = 'debit'
		  AND bt.created_at >= ?
		  AND bt.created_at <= ?
	`

	args := []interface{}{startDate, endDate}
	if workspaceID != nil && strings.TrimSpace(*workspaceID) != "" {
		query += ` AND bt.workspace_id = ?`
		args = append(args, strings.TrimSpace(*workspaceID))
	}
	query += ` ORDER BY bt.created_at DESC LIMIT ?`
	args = append(args, limit)

	var rows []recentSpendingRow
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("analytics recent spendings: %w", err)
	}

	items := make([]analytics_domain.RecentSpending, 0, len(rows))
	for _, row := range rows {
		items = append(items, analytics_domain.RecentSpending{
			TransactionID:      row.TransactionID,
			WorkspaceID:        row.WorkspaceID,
			WorkspaceName:      row.WorkspaceName,
			ServiceType:        row.ServiceType,
			Description:        row.Description,
			AmountMicros:       row.AmountMicros,
			CostMicros:         row.CostMicros,
			ProfitMicros:       row.ProfitMicros,
			ExchangeRateMicros: row.ExchangeRateMicros,
			BalanceAfterMicros: row.BalanceAfterMicros,
			CreatedAt:          row.CreatedAt,
		})
	}

	return items, nil
}

func (r *repository) getProductUsageSummary(workspaceID *string, startDate, endDate time.Time) (*analytics_domain.ProductUsageSummary, error) {
	serviceQuery := `
		SELECT
			service_type AS key,
			COALESCE(SUM(CASE WHEN type = 'debit' THEN 1 ELSE -1 END), 0) AS count,
			COALESCE(SUM(CASE WHEN type = 'debit' THEN amount ELSE -amount END), 0) AS revenue_micros,
			COALESCE(SUM(CASE WHEN type = 'debit' THEN amount * exchange_rate_micros / 1000000 ELSE -amount * exchange_rate_micros / 1000000 END), 0) AS revenue_brl_micros
		FROM balance_transactions
		WHERE ((type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true))
		  AND created_at >= ?
		  AND created_at <= ?
	`
	serviceArgs := []interface{}{startDate, endDate}
	if workspaceID != nil && strings.TrimSpace(*workspaceID) != "" {
		serviceQuery += ` AND workspace_id = ?`
		serviceArgs = append(serviceArgs, strings.TrimSpace(*workspaceID))
	}
	serviceQuery += ` GROUP BY service_type ORDER BY revenue_micros DESC, count DESC`

	var serviceRows []usageMetricRow
	if err := r.db.Raw(serviceQuery, serviceArgs...).Scan(&serviceRows).Error; err != nil {
		return nil, fmt.Errorf("analytics product usage by service: %w", err)
	}

	whatsAppCategoryQuery := `
		SELECT
			COALESCE(NULLIF(UPPER(TRIM(e.metadata->'template_info'->>'category')), ''), 'UNKNOWN') AS key,
			COUNT(*) AS count
		FROM whatsapp_campaign_entries e
		JOIN whatsapp_campaigns c ON c.id = e.campaign_id
		WHERE e.deleted_at IS NULL
		  AND c.deleted_at IS NULL
		  AND e.created_at >= ?
		  AND e.created_at <= ?
		  AND e.status IN ('SENT', 'DELIVERED', 'READ')
	`
	categoryArgs := []interface{}{startDate, endDate}
	if workspaceID != nil && strings.TrimSpace(*workspaceID) != "" {
		whatsAppCategoryQuery += ` AND c.workspace_id = ?`
		categoryArgs = append(categoryArgs, strings.TrimSpace(*workspaceID))
	}
	whatsAppCategoryQuery += ` GROUP BY key ORDER BY count DESC`

	var categoryRows []categoryUsageRow
	if err := r.db.Raw(whatsAppCategoryQuery, categoryArgs...).Scan(&categoryRows).Error; err != nil {
		return nil, fmt.Errorf("analytics whatsapp template categories: %w", err)
	}

	return &analytics_domain.ProductUsageSummary{
		ByService:                  mapUsageMetricRows(serviceRows),
		WhatsAppTemplateCategories: mapCategoryUsageRows(categoryRows),
	}, nil
}

func mapUsageMetricRows(rows []usageMetricRow) []analytics_domain.UsageMetric {
	items := make([]analytics_domain.UsageMetric, 0, len(rows))
	for _, row := range rows {
		items = append(items, analytics_domain.UsageMetric{
			Key:              row.Key,
			Count:            row.Count,
			RevenueMicros:    row.RevenueMicros,
			RevenueBRLMicros: row.RevenueBRLMicros,
		})
	}
	return items
}

func mapCategoryUsageRows(rows []categoryUsageRow) []analytics_domain.UsageMetric {
	items := make([]analytics_domain.UsageMetric, 0, len(rows))
	for _, row := range rows {
		items = append(items, analytics_domain.UsageMetric{
			Key:   row.Key,
			Count: row.Count,
		})
	}
	return items
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func normalizeOverviewSortField(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "workspace", "workspace_name", "name":
		return "workspace_name"
	case "balance", "current_balance", "current_balance_micros":
		return "current_balance_micros"
	case "credits", "period_credits", "period_credits_micros":
		return "period_credits_micros"
	case "cost", "cost_micros":
		return "cost_micros"
	case "profit", "profit_micros":
		return "profit_micros"
	case "transactions", "tx_count":
		return "tx_count"
	case "last_transaction", "last_transaction_at":
		return "last_transaction_at"
	default:
		return "revenue_micros"
	}
}

func normalizeOverviewSortDirection(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "asc") {
		return "ASC"
	}
	return "DESC"
}
