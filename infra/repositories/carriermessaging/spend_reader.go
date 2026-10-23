package carriermessaging_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/balance"
)

const settledLedgerRows = `((type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true))`

var carrierLedgerServices = []string{string(balance.ServiceRCS), string(balance.ServiceSMS)}

type SpendReader struct {
	db *gorm.DB
}

func NewSpendReader(db *gorm.DB) *SpendReader {
	return &SpendReader{db: db}
}

func (r *SpendReader) SpendSince(ctx context.Context, workspaceID string, since time.Time) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Raw(
		`SELECT COALESCE(SUM(CASE WHEN type = 'debit' THEN amount ELSE -amount END), 0) FROM balance_transactions
		WHERE workspace_id = ? AND service_type IN ? AND created_at >= ? AND `+settledLedgerRows,
		workspaceID, carrierLedgerServices, since,
	).Scan(&total).Error
	return total, err
}
