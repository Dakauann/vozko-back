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

func NewMonthlySendCapRepository(db *gorm.DB) balance.MonthlySendCapRepository {
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
	return r.db.Where("workspace_id = ?", workspaceID).Delete(&schema.WorkspaceMonthlySendCap{}).Error
}

func (r *MonthlySendCapRepository) ListMonthlySendCapUsage(since time.Time) ([]balance.SendCapUsage, error) {
	type usageRow struct {
		schema.WorkspaceMonthlySendCap
		WorkspaceName string
		Used          int64
	}
	var rows []usageRow
	sql := `SELECT c.workspace_id, c.monthly_limit, c.updated_by, c.updated_at, c.unlocked_by, c.unlocked_at, w.name AS workspace_name, (` +
		netTemplateSendsSinceSQL("c.workspace_id") + `) AS used
		FROM workspace_monthly_send_caps c JOIN workspaces w ON w.id = c.workspace_id AND w.deleted_at IS NULL`
	if err := r.db.Raw(sql, since).Scan(&rows).Error; err != nil {
		return nil, err
	}
	usages := make([]balance.SendCapUsage, 0, len(rows))
	for _, row := range rows {
		usages = append(usages, balance.SendCapUsage{
			Cap:           toDomainMonthlySendCap(row.WorkspaceMonthlySendCap),
			WorkspaceName: row.WorkspaceName,
			Used:          row.Used,
		})
	}
	return usages, nil
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
