package balance_repository

import (
	"gorm.io/gorm"

	"vozko/domain/balance"
)

func NewReferenceNet(db *gorm.DB) balance.ReferenceNetReader {
	return &BalanceRepositoryImpl{db: db}
}

const netCreditUnderSQL = `
SELECT COALESCE(SUM(CASE WHEN type = 'credit' THEN amount ELSE -amount END), 0)
FROM balance_transactions
WHERE workspace_id = ? AND reference_id = ?`

func (r *BalanceRepositoryImpl) NetCreditUnder(workspaceID, referenceID string) (int64, error) {
	var net int64
	err := r.db.Raw(netCreditUnderSQL, workspaceID, referenceID).Scan(&net).Error
	return net, err
}

var _ balance.ReferenceNetReader = (*BalanceRepositoryImpl)(nil)
