package balance_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/balance"
	"vozko/infra/database/schema"
)

type MonthlySendCapRepository struct {
	db *gorm.DB
}

func NewMonthlySendCapRepository(db *gorm.DB) *MonthlySendCapRepository {
	return &MonthlySendCapRepository{db: db}
}

func (r *MonthlySendCapRepository) GetMonthlySendCap(workspaceID string) (*balance.MonthlySendCap, error) {
	var row schema.WorkspaceMonthlySendCap
	err := r.db.Where("workspace_id = ?", workspaceID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cap := toDomainMonthlySendCap(row)
	return &cap, nil
}

func (r *MonthlySendCapRepository) UpsertMonthlySendCap(cap balance.MonthlySendCap) error {
	row := schema.WorkspaceMonthlySendCap{
		WorkspaceID:  cap.WorkspaceID,
		MonthlyLimit: cap.Limit,
		UpdatedBy:    cap.UpdatedBy,
		UpdatedAt:    cap.UpdatedAt,
		UnlockedBy:   cap.UnlockedBy,
		UnlockedAt:   cap.UnlockedAt,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"monthly_limit", "updated_by", "updated_at", "unlocked_by", "unlocked_at"}),
	}).Create(&row).Error
}

func (r *MonthlySendCapRepository) DeleteMonthlySendCap(workspaceID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("workspace_id = ?", workspaceID).Delete(&schema.WorkspaceMonthlySendCap{}).Error; err != nil {
			return err
		}
		return tx.Where("workspace_id = ?", workspaceID).Delete(&schema.WorkspaceMonthlySendSlot{}).Error
	})
}

func (r *MonthlySendCapRepository) TakeMonthlySendSlot(workspaceID, referenceID string, period time.Time) (bool, error) {
	took := false
	var refusal error
	err := r.db.Transaction(func(tx *gorm.DB) error {
		cap, err := lockMonthlySendCap(tx, workspaceID)
		if err != nil || cap == nil {
			return err
		}
		taken, err := slotTaken(tx, referenceID)
		if err != nil || taken {
			return err
		}
		counted, used, err := usedInOpenPeriod(tx, cap, period)
		if err != nil {
			return err
		}
		if refusal = toDomainMonthlySendCap(*cap).CheckRoom(used); refusal != nil {
			return saveMonthlySendCount(tx, workspaceID, counted, used)
		}
		slot := schema.WorkspaceMonthlySendSlot{ReferenceID: referenceID, WorkspaceID: workspaceID, Period: counted}
		if err := tx.Create(&slot).Error; err != nil {
			return err
		}
		if err := saveMonthlySendCount(tx, workspaceID, counted, used+1); err != nil {
			return err
		}
		took = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return took, refusal
}

func (r *MonthlySendCapRepository) GiveBackMonthlySendSlot(workspaceID, referenceID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		cap, err := lockMonthlySendCap(tx, workspaceID)
		if err != nil {
			return err
		}
		var slots []schema.WorkspaceMonthlySendSlot
		if err := tx.Where("reference_id = ? AND workspace_id = ?", referenceID, workspaceID).Limit(1).Find(&slots).Error; err != nil {
			return err
		}
		if len(slots) == 0 {
			return nil
		}
		if err := tx.Where("reference_id = ?", referenceID).Delete(&schema.WorkspaceMonthlySendSlot{}).Error; err != nil {
			return err
		}
		if cap == nil || !countedIn(cap, slots[0].Period) || cap.Used <= 0 {
			return nil
		}
		return saveMonthlySendCount(tx, workspaceID, slots[0].Period, cap.Used-1)
	})
}

func (r *MonthlySendCapRepository) ListMonthlySendCapUsage(since time.Time) ([]balance.SendCapUsage, error) {
	type usageRow struct {
		schema.WorkspaceMonthlySendCap
		WorkspaceName string
		MonthUsed     int64
	}
	var rows []usageRow
	sql := `SELECT c.workspace_id, c.monthly_limit, c.updated_by, c.updated_at, c.unlocked_by, c.unlocked_at, w.name AS workspace_name,
		CASE WHEN c.counted_from >= ? THEN c.used WHEN c.counted_from IS NULL THEN (` + netTemplateSendsSinceSQL("c.workspace_id") + `) ELSE 0 END AS month_used
		FROM workspace_monthly_send_caps c JOIN workspaces w ON w.id = c.workspace_id AND w.deleted_at IS NULL`
	if err := r.db.Raw(sql, since, since).Scan(&rows).Error; err != nil {
		return nil, err
	}
	usages := make([]balance.SendCapUsage, 0, len(rows))
	for _, row := range rows {
		usages = append(usages, balance.SendCapUsage{
			Cap:           toDomainMonthlySendCap(row.WorkspaceMonthlySendCap),
			WorkspaceName: row.WorkspaceName,
			Used:          row.MonthUsed,
		})
	}
	return usages, nil
}

func lockMonthlySendCap(tx *gorm.DB, workspaceID string) (*schema.WorkspaceMonthlySendCap, error) {
	var rows []schema.WorkspaceMonthlySendCap
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ?", workspaceID).
		Limit(1).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func slotTaken(tx *gorm.DB, referenceID string) (bool, error) {
	var count int64
	err := tx.Model(&schema.WorkspaceMonthlySendSlot{}).Where("reference_id = ?", referenceID).Count(&count).Error
	return count > 0, err
}

func countedIn(cap *schema.WorkspaceMonthlySendCap, period time.Time) bool {
	return cap.CountedFrom != nil && cap.CountedFrom.Equal(period)
}

func usedInOpenPeriod(tx *gorm.DB, cap *schema.WorkspaceMonthlySendCap, period time.Time) (time.Time, int64, error) {
	switch {
	case cap.CountedFrom == nil:
		var used int64
		err := tx.Raw(netTemplateSendsSinceSQL("?"), cap.WorkspaceID, period).Scan(&used).Error
		return period, used, err
	case !period.After(*cap.CountedFrom):
		return *cap.CountedFrom, cap.Used, nil
	default:
		err := tx.Where("workspace_id = ? AND period < ?", cap.WorkspaceID, period).Delete(&schema.WorkspaceMonthlySendSlot{}).Error
		return period, 0, err
	}
}

func saveMonthlySendCount(tx *gorm.DB, workspaceID string, period time.Time, used int64) error {
	return tx.Model(&schema.WorkspaceMonthlySendCap{}).
		Where("workspace_id = ?", workspaceID).
		Updates(map[string]interface{}{"counted_from": period, "used": used}).Error
}

func toDomainMonthlySendCap(row schema.WorkspaceMonthlySendCap) balance.MonthlySendCap {
	return balance.MonthlySendCap{
		WorkspaceID: row.WorkspaceID,
		Limit:       row.MonthlyLimit,
		UpdatedBy:   row.UpdatedBy,
		UpdatedAt:   row.UpdatedAt,
		UnlockedBy:  row.UnlockedBy,
		UnlockedAt:  row.UnlockedAt,
	}
}
