package emailsending_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

const takeDailySlotSQL = `INSERT INTO email_daily_usage (domain_id, day, used, cap, updated_at)
VALUES (?, ?, 1, ?, NOW())
ON CONFLICT (domain_id, day) DO UPDATE
SET used = email_daily_usage.used + 1, cap = EXCLUDED.cap, updated_at = NOW()
WHERE EXCLUDED.cap = -1 OR email_daily_usage.used < EXCLUDED.cap
RETURNING used`

const giveBackDailySlotSQL = `UPDATE email_daily_usage SET used = used - 1, updated_at = NOW()
WHERE domain_id = ? AND day = ? AND used > 0`

type usageRepository struct {
	db *gorm.DB
}

func NewDailyUsageRepository(db *gorm.DB) es.DailyUsage {
	return &usageRepository{db: db}
}

func (r *usageRepository) TakeDailySlot(ctx context.Context, domainID, day string, cap int64) (bool, error) {
	if cap == 0 || cap < es.Uncapped {
		return false, nil
	}
	var used []int64
	if err := r.db.WithContext(ctx).Raw(takeDailySlotSQL, domainID, day, cap).Scan(&used).Error; err != nil {
		return false, err
	}
	return len(used) == 1, nil
}

func (r *usageRepository) GiveBackDailySlot(ctx context.Context, domainID, day string) error {
	return r.db.WithContext(ctx).Exec(giveBackDailySlotSQL, domainID, day).Error
}

func (r *usageRepository) Used(ctx context.Context, domainID, day string) (int64, error) {
	var row schema.EmailDailyUsage
	err := r.db.WithContext(ctx).Where("domain_id = ? AND day = ?", domainID, day).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.Used, nil
}
