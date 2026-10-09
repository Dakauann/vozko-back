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

func (r *MonthlySendCapRepository) MonthlySendCapUsage(workspaceID string, at time.Time) (*balance.SendCapUsage, error) {
	var rows []schema.WorkspaceMonthlySendCap
	if err := r.db.Where("workspace_id = ?", workspaceID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	cap := toDomainMonthlySendCap(rows[0])
	cycleStart := cap.CycleStart(at)
	used, err := usedInCycle(r.db, &rows[0], cycleStart)
	if err != nil {
		return nil, err
	}
	return &balance.SendCapUsage{Cap: cap, Used: used, CycleStart: cycleStart}, nil
}

func (r *MonthlySendCapRepository) UpsertMonthlySendCap(cap balance.MonthlySendCap) error {
	row := schema.WorkspaceMonthlySendCap{
		WorkspaceID:  cap.WorkspaceID,
		MonthlyLimit: cap.Limit,
		CycleDay:     cap.CycleDay,
		UpdatedBy:    cap.UpdatedBy,
		UpdatedAt:    cap.UpdatedAt,
		UnlockedBy:   cap.UnlockedBy,
		UnlockedAt:   cap.UnlockedAt,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "workspace_id"}},
		DoUpdates: append(
			clause.AssignmentColumns([]string{"monthly_limit", "cycle_day", "updated_by", "updated_at", "unlocked_by", "unlocked_at"}),
			clause.Assignment{Column: clause.Column{Name: "counted_from"}, Value: gorm.Expr("CASE WHEN workspace_monthly_send_caps.cycle_day = excluded.cycle_day THEN workspace_monthly_send_caps.counted_from END")},
		),
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

func (r *MonthlySendCapRepository) TakeMonthlySendSlot(workspaceID, referenceID string, at time.Time) (bool, error) {
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
		counted, used, err := usedInOpenPeriod(tx, cap, toDomainMonthlySendCap(*cap).CycleStart(at))
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

func (r *MonthlySendCapRepository) ListMonthlySendCapUsage(at time.Time) ([]balance.SendCapUsage, error) {
	type capRow struct {
		schema.WorkspaceMonthlySendCap
		WorkspaceName string
	}
	var rows []capRow
	if err := r.db.Raw(`SELECT c.*, w.name AS workspace_name FROM workspace_monthly_send_caps c JOIN workspaces w ON w.id = c.workspace_id AND w.deleted_at IS NULL`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	usages := make([]balance.SendCapUsage, 0, len(rows))
	for _, row := range rows {
		cap := toDomainMonthlySendCap(row.WorkspaceMonthlySendCap)
		cycleStart := cap.CycleStart(at)
		used, err := usedInCycle(r.db, &row.WorkspaceMonthlySendCap, cycleStart)
		if err != nil {
			return nil, err
		}
		usages = append(usages, balance.SendCapUsage{Cap: cap, WorkspaceName: row.WorkspaceName, Used: used, CycleStart: cycleStart})
	}
	return usages, nil
}

func usedInCycle(db *gorm.DB, cap *schema.WorkspaceMonthlySendCap, cycleStart time.Time) (int64, error) {
	switch {
	case cap.CountedFrom == nil:
		var used int64
		err := db.Raw(netTemplateSendsSinceSQL("?"), cap.WorkspaceID, cycleStart).Scan(&used).Error
		return used, err
	case cycleStart.After(*cap.CountedFrom):
		return 0, nil
	default:
		return cap.Used, nil
	}
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
		CycleDay:    row.CycleDay,
		UpdatedBy:   row.UpdatedBy,
		UpdatedAt:   row.UpdatedAt,
		UnlockedBy:  row.UnlockedBy,
		UnlockedAt:  row.UnlockedAt,
	}
}
