package balance_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/balance"
	"vozko/infra/database/schema"
)

type ChargeClock struct {
	db *gorm.DB
}

var _ balance.ChargeClock = (*ChargeClock)(nil)

func NewChargeClock(db *gorm.DB) *ChargeClock {
	return &ChargeClock{db: db}
}

func (c *ChargeClock) ChargedAt(referenceID string) (time.Time, bool, error) {
	var charges []schema.BalanceTransaction
	err := c.db.Select("created_at").
		Where("reference_id = ? AND type = ?", referenceID, string(balance.TransactionTypeDebit)).
		Order("created_at").
		Limit(1).
		Find(&charges).Error
	if err != nil || len(charges) == 0 {
		return time.Time{}, false, err
	}
	return charges[0].CreatedAt, true, nil
}
