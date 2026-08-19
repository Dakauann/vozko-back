package balance_repository

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/balance"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

const defaultExchangeRateMicros int64 = 6_000_000

type BalanceRepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) balance.Repository {
	return &BalanceRepositoryImpl{db: db}
}

func (r *BalanceRepositoryImpl) resolveExchangeRate(txDB *gorm.DB, provided int64) int64 {
	if provided != 0 {
		return provided
	}
	var priceMicros int64
	err := txDB.Table("pricing_items").
		Select("price_micros").
		Where("workspace_id IS NULL AND category = ? AND service = ? AND metric = ?",
			"exchange_rate", "usd_to_brl", "per_unit").
		Limit(1).
		Scan(&priceMicros).Error
	if err != nil || priceMicros <= 0 {
		return defaultExchangeRateMicros
	}
	return priceMicros
}

func (r *BalanceRepositoryImpl) Create(b *balance.Balance) error {
	existing, _ := r.GetByWorkspaceID(b.WorkspaceID)
	if existing != nil {
		return balance.ErrWorkspaceAlreadyHasBalance
	}

	dbBalance := &schema.Balance{
		ID:          b.ID,
		WorkspaceID: b.WorkspaceID,
		Amount:      b.Amount,
		Currency:    b.Currency,
	}

	if err := r.db.Create(dbBalance).Error; err != nil {
		return err
	}

	b.CreatedAt = dbBalance.CreatedAt
	b.UpdatedAt = dbBalance.UpdatedAt
	return nil
}

// ListWorkspacesBelowBalance returns funded wallets at or below thresholdMicros in
// a single indexed scan (one balance row per workspace). amount > 0 excludes
// never-funded and fully-drained wallets so only paying customers are warned.
func (r *BalanceRepositoryImpl) ListWorkspacesBelowBalance(thresholdMicros int64) ([]balance.LowBalanceRow, error) {
	var rows []balance.LowBalanceRow
	err := r.db.Model(&schema.Balance{}).
		Select("workspace_id, amount AS amount_micros, currency").
		Where("amount > 0 AND amount <= ?", thresholdMicros).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

var _ balance.LowBalanceLister = (*BalanceRepositoryImpl)(nil)

func (r *BalanceRepositoryImpl) GetByWorkspaceID(workspaceID string) (*balance.Balance, error) {
	var dbBalance schema.Balance
	if err := r.db.Where("workspace_id = ?", workspaceID).First(&dbBalance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, balance.ErrBalanceNotFound
		}
		return nil, err
	}
	return mapBalanceToDomain(&dbBalance), nil
}

func (r *BalanceRepositoryImpl) CreditBalance(params balance.CreditBalanceInput) (*balance.Transaction, error) {
	if params.Amount <= 0 {
		return nil, balance.ErrInvalidAmount
	}

	var transaction *balance.Transaction
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var dbBalance schema.Balance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ?", params.WorkspaceID).
			First(&dbBalance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return balance.ErrBalanceNotFound
			}
			return err
		}

		balanceBefore := dbBalance.Amount
		balanceAfter := balanceBefore + params.Amount

		dbTransaction := &schema.BalanceTransaction{
			ID:                 uuid.New().String(),
			BalanceID:          dbBalance.ID,
			WorkspaceID:        params.WorkspaceID,
			Type:               string(balance.TransactionTypeCredit),
			Amount:             params.Amount,
			ResourceType:       string(balance.ResourceTypeMoney),
			BalanceBefore:      balanceBefore,
			BalanceAfter:       balanceAfter,
			ServiceType:        string(params.ServiceType),
			ReferenceID:        params.ReferenceID,
			Description:        params.Description,
			CostMicros:         params.CostMicros,
			ProfitMicros:       params.ProfitMicros,
			ExchangeRateMicros: r.resolveExchangeRate(tx, params.ExchangeRateMicros),
			IsRefund:           params.IsRefund,
		}

		if err := tx.Create(dbTransaction).Error; err != nil {
			return err
		}

		if err := tx.Model(&dbBalance).Update("amount", balanceAfter).Error; err != nil {
			return err
		}

		transaction = mapTransactionToDomain(dbTransaction)
		return nil
	})

	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (r *BalanceRepositoryImpl) DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error) {
	if params.Amount <= 0 {
		return nil, balance.ErrInvalidAmount
	}

	var transaction *balance.Transaction
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var dbBalance schema.Balance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ?", params.WorkspaceID).
			First(&dbBalance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return balance.ErrBalanceNotFound
			}
			return err
		}

		balanceBefore := dbBalance.Amount
		if !params.AllowNegative && balanceBefore < params.Amount {
			return balance.ErrInsufficientBalance
		}
		balanceAfter := balanceBefore - params.Amount

		dbTransaction := &schema.BalanceTransaction{
			ID:                 uuid.New().String(),
			BalanceID:          dbBalance.ID,
			WorkspaceID:        params.WorkspaceID,
			Type:               string(balance.TransactionTypeDebit),
			Amount:             params.Amount,
			ResourceType:       string(balance.ResourceTypeMoney),
			BalanceBefore:      balanceBefore,
			BalanceAfter:       balanceAfter,
			ServiceType:        string(params.ServiceType),
			ReferenceID:        params.ReferenceID,
			Description:        params.Description,
			CostMicros:         params.CostMicros,
			ProfitMicros:       params.ProfitMicros,
			ExchangeRateMicros: r.resolveExchangeRate(tx, params.ExchangeRateMicros),
			IsRefund:           params.IsRefund,
		}

		if err := tx.Create(dbTransaction).Error; err != nil {
			return err
		}

		if err := tx.Model(&dbBalance).Update("amount", balanceAfter).Error; err != nil {
			return err
		}

		transaction = mapTransactionToDomain(dbTransaction)
		return nil
	})

	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (r *BalanceRepositoryImpl) HasSufficientBalance(workspaceID string, amount int64) (bool, error) {
	var hasSufficient bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var dbBalance schema.Balance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ?", workspaceID).
			First(&dbBalance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return balance.ErrBalanceNotFound
			}
			return err
		}

		hasSufficient = dbBalance.Amount >= amount
		return nil
	})

	if err != nil {
		return false, err
	}
	return hasSufficient, nil
}

func (r *BalanceRepositoryImpl) GetFullBalanceSummary(workspaceID string) (*balance.FullBalanceSummary, error) {
	bal, err := r.GetByWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}

	type totals struct {
		ResourceType string `gorm:"column:resource_type"`
		TxType       string `gorm:"column:type"`
		Total        int64  `gorm:"column:total"`
	}

	var results []totals
	if err := r.db.Model(&schema.BalanceTransaction{}).
		Select("resource_type, type, COALESCE(SUM(amount), 0) as total").
		Where("balance_id = ?", bal.ID).
		Group("resource_type, type").
		Find(&results).Error; err != nil {
		return nil, err
	}

	summary := &balance.FullBalanceSummary{
		Balance: bal,
	}

	for _, row := range results {
		if balance.ResourceType(row.ResourceType) == balance.ResourceTypeMoney {
			if row.TxType == string(balance.TransactionTypeCredit) {
				summary.TotalMoneyCredits = row.Total
			} else {
				summary.TotalMoneyDebits = row.Total
			}
		}
	}

	return summary, nil
}

func (r *BalanceRepositoryImpl) GetTransaction(transactionID string) (*balance.Transaction, error) {
	var dbTransaction schema.BalanceTransaction
	if err := r.db.Where("id = ?", transactionID).First(&dbTransaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, balance.ErrTransactionNotFound
		}
		return nil, err
	}
	return mapTransactionToDomain(&dbTransaction), nil
}

func (r *BalanceRepositoryImpl) ListTransactions(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.BalanceTransaction{})

	if input.WorkspaceID != "" {
		query = query.Where("balance_id IN (SELECT id FROM balances WHERE workspace_id = ?)", input.WorkspaceID)
	}
	if input.ServiceType != nil {
		query = query.Where("service_type = ?", string(*input.ServiceType))
	}
	if input.Type != nil {
		query = query.Where("type = ?", string(*input.Type))
	}
	if input.ResourceType != nil {
		query = query.Where("resource_type = ?", string(*input.ResourceType))
	}
	if input.StartDate != nil {
		query = query.Where("created_at >= ?", *input.StartDate)
	}
	if input.EndDate != nil {
		query = query.Where("created_at <= ?", *input.EndDate)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbTransactions []schema.BalanceTransaction
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbTransactions).Error; err != nil {
		return nil, err
	}

	transactions := make([]*balance.Transaction, len(dbTransactions))
	for i := range dbTransactions {
		transactions[i] = mapTransactionToDomain(&dbTransactions[i])
	}

	return shared.NewPaginatedResult(transactions, pagination, total), nil
}

func (r *BalanceRepositoryImpl) ExistsTransactionByReferenceID(referenceID string) (bool, error) {
	var exists bool
	err := r.db.Model(&schema.BalanceTransaction{}).
		Select("1").
		Where("reference_id = ?", referenceID).
		Limit(1).
		Find(&exists).Error
	return exists, err
}

// AggregateWhatsAppTemplateCharges returns net billed template sends from the
// balance ledger (debits − refunds). Date filter is transaction created_at so
// campaign reset does not erase the count.
//
// Performance notes (see indexes.go idx_bt_ws_svc_created_charges):
//   - Predicates are ordered workspace_id → service_type → created_at range so
//     the composite partial index can drive an index-only range scan.
//   - Avoid COALESCE on is_refund (column is NOT NULL) so the planner can match
//     simple boolean predicates.
//   - Category is derived from description only after the index filters rows for
//     one workspace + service + period (small set). A dedicated category column
//     would be better long-term; description parse is acceptable post-filter.
//   - Campaign type/department use EXISTS against whatsapp_campaigns PK / dept
//     index, not a join expression on substring(reference_id), so the campaign
//     side stays indexable.
func (r *BalanceRepositoryImpl) AggregateWhatsAppTemplateCharges(filter balance.WhatsAppChargeFilter) (*balance.WhatsAppChargeStats, error) {
	stats := &balance.WhatsAppChargeStats{}
	ws := strings.TrimSpace(filter.WorkspaceID)
	if ws == "" {
		return stats, nil
	}

	// Debit descriptions: "Template WhatsApp utility (ref: …)"
	// Refund credits: "Reembolso: template WhatsApp utility …" + reference refund:<id>
	// position() is sargable-free but runs only on the index-filtered subset.
	sql := `
		SELECT
			CASE
				WHEN position('marketing' IN lower(bt.description)) > 0 THEN 'MARKETING'
				WHEN position('authentication' IN lower(bt.description)) > 0 THEN 'AUTHENTICATION'
				WHEN position('utility' IN lower(bt.description)) > 0 THEN 'UTILITY'
				ELSE 'UNKNOWN'
			END AS category,
			COALESCE(SUM(
				CASE
					WHEN bt.type = 'debit'  AND bt.is_refund = false THEN 1
					WHEN bt.type = 'credit' AND bt.is_refund = true  THEN -1
					ELSE 0
				END
			), 0)::bigint AS net
		FROM balance_transactions bt
		WHERE bt.workspace_id = ?
		  AND bt.service_type = ?
		  AND (
		    (bt.type = 'debit'  AND bt.is_refund = false)
		    OR (bt.type = 'credit' AND bt.is_refund = true)
		  )
	`
	args := []interface{}{ws, string(balance.ServiceWhatsAppCampaign)}
	if filter.From != nil {
		sql += ` AND bt.created_at >= ?`
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		sql += ` AND bt.created_at <= ?`
		args = append(args, *filter.To)
	}

	// Scope to campaigns matching type/department without materializing a CTE.
	// Two EXISTS branches cover debit reference_id = campaign.id and refund
	// reference_id = 'refund:' || campaign.id (both equality matches).
	needCampaign := strings.TrimSpace(filter.CampaignType) != "" || len(filter.DepartmentIDs) > 0
	if needCampaign {
		campConds := []string{`c.workspace_id = ?`, `c.deleted_at IS NULL`}
		campArgs := []interface{}{ws}
		if t := strings.TrimSpace(filter.CampaignType); t != "" {
			campConds = append(campConds, `c.type = ?`)
			campArgs = append(campArgs, t)
		}
		if len(filter.DepartmentIDs) > 0 {
			campConds = append(campConds, `c.department_id IN ?`)
			campArgs = append(campArgs, filter.DepartmentIDs)
		}
		whereCamp := strings.Join(campConds, ` AND `)
		sql += `
		  AND (
		    EXISTS (
		      SELECT 1 FROM whatsapp_campaigns c
		      -- c.id is uuid, bt.reference_id is varchar: compare as text or
		      -- Postgres raises 42883 (operator does not exist: uuid = varchar).
		      WHERE c.id::text = bt.reference_id AND ` + whereCamp + `
		    )
		    OR EXISTS (
		      SELECT 1 FROM whatsapp_campaigns c
		      WHERE bt.reference_id = ('refund:' || c.id) AND ` + whereCamp + `
		    )
		    -- Single-target sends reference their send ATTEMPT, not a campaign, so
		    -- the two branches above cannot see them and a department-filtered
		    -- report would silently omit money the workspace actually spent. The
		    -- attempt carries the campaign it was sent into, which is what puts it
		    -- back under the right department.
		    OR EXISTS (
		      SELECT 1 FROM whatsapp_template_sends s
		      JOIN whatsapp_campaigns c ON c.id = s.campaign_id
		      WHERE bt.reference_id = ('waba:' || s.id) AND s.deleted_at IS NULL AND ` + whereCamp + `
		    )
		    OR EXISTS (
		      SELECT 1 FROM whatsapp_template_sends s
		      JOIN whatsapp_campaigns c ON c.id = s.campaign_id
		      WHERE bt.reference_id = ('refund:waba:' || s.id) AND s.deleted_at IS NULL AND ` + whereCamp + `
		    )
		  )
		`
		// One copy of campArgs per EXISTS branch, in order.
		for i := 0; i < 4; i++ {
			args = append(args, campArgs...)
		}
	}

	sql += ` GROUP BY 1`

	type row struct {
		Category string
		Net      int64
	}
	var rows []row
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	var total int64
	for _, rw := range rows {
		n := rw.Net
		if n < 0 {
			n = 0
		}
		switch strings.ToUpper(strings.TrimSpace(rw.Category)) {
		case "MARKETING":
			stats.Marketing = n
			total += n
		case "UTILITY":
			stats.Utility = n
			total += n
		case "AUTHENTICATION":
			stats.Authentication = n
			total += n
		default:
			// Keep unknown in total so Envios still reflects all charged sends.
			total += n
		}
	}
	stats.NetDispatches = total
	return stats, nil
}

func (r *BalanceRepositoryImpl) EnsureBalanceExists(workspaceID string, currency string) (*balance.Balance, error) {
	existing, err := r.GetByWorkspaceID(workspaceID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, balance.ErrBalanceNotFound) {
		return nil, err
	}

	newBalance := &balance.Balance{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Amount:      0,
		Currency:    strings.ToUpper(currency),
	}
	if err := r.Create(newBalance); err != nil {
		if errors.Is(err, balance.ErrWorkspaceAlreadyHasBalance) {
			return r.GetByWorkspaceID(workspaceID)
		}
		return nil, err
	}
	return newBalance, nil
}

func mapBalanceToDomain(db *schema.Balance) *balance.Balance {
	return &balance.Balance{
		ID:          db.ID,
		WorkspaceID: db.WorkspaceID,
		Amount:      db.Amount,
		Currency:    db.Currency,
		CreatedAt:   db.CreatedAt,
		UpdatedAt:   db.UpdatedAt,
	}
}

func mapTransactionToDomain(db *schema.BalanceTransaction) *balance.Transaction {
	return &balance.Transaction{
		ID:                 db.ID,
		BalanceID:          db.BalanceID,
		WorkspaceID:        db.WorkspaceID,
		Type:               balance.TransactionType(db.Type),
		Amount:             db.Amount,
		ResourceType:       balance.ResourceType(db.ResourceType),
		BalanceBefore:      db.BalanceBefore,
		BalanceAfter:       db.BalanceAfter,
		ServiceType:        balance.ServiceType(db.ServiceType),
		ReferenceID:        db.ReferenceID,
		Description:        db.Description,
		CostMicros:         db.CostMicros,
		ProfitMicros:       db.ProfitMicros,
		ExchangeRateMicros: db.ExchangeRateMicros,
		IsRefund:           db.IsRefund,
		CreatedAt:          db.CreatedAt,
	}
}

func (r *BalanceRepositoryImpl) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	type row struct {
		WorkspaceID  string `gorm:"column:workspace_id"`
		ServiceType  string `gorm:"column:service_type"`
		CostMicros   int64  `gorm:"column:cost_micros"`
		MessageCount int    `gorm:"column:message_count"`
	}

	var rows []row
	err := r.db.Model(&schema.BalanceTransaction{}).
		Select(`workspace_id, service_type,
			COALESCE(SUM(CASE WHEN type = 'debit' AND is_refund = false THEN cost_micros
			                  WHEN type = 'credit' AND is_refund = true THEN -cost_micros
			                  ELSE 0 END), 0) as cost_micros,
			COALESCE(SUM(CASE WHEN type = 'debit' AND is_refund = false THEN 1
			                  WHEN type = 'credit' AND is_refund = true THEN -1
			                  ELSE 0 END), 0) as message_count`).
		Where("((type = ? AND is_refund = ?) OR (type = ? AND is_refund = ?)) AND created_at >= ? AND created_at < ?",
			string(balance.TransactionTypeDebit), false,
			string(balance.TransactionTypeCredit), true,
			start, end).
		Group("workspace_id, service_type").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]balance.DailyCostRow, 0, len(rows))
	for _, r := range rows {

		if r.CostMicros <= 0 {
			continue
		}
		out = append(out, balance.DailyCostRow{
			WorkspaceID:  r.WorkspaceID,
			ServiceType:  r.ServiceType,
			CostMicros:   r.CostMicros,
			MessageCount: r.MessageCount,
		})
	}
	return out, nil
}
