package mercadolivre_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

const exportPageSize = 1000

type exportRepository struct {
	db       *gorm.DB
	pageSize int
}

func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db, pageSize: exportPageSize}
}

type exportRow struct {
	EntryID   string    `gorm:"column:entry_id"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
	Nickname  string    `gorm:"column:nickname"`
	MLUserID  string    `gorm:"column:ml_user_id"`
	PackKey   string    `gorm:"column:pack_key"`
}

func (r *exportRepository) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}
	var after *exportRow
	for {
		query := r.db.WithContext(ctx).
			Table("mercadolivre_conversations mlc").
			Select(`mlc.id AS entry_id,
				COALESCE(NULLIF(mlc.conversation_status, ''), 'new') AS status,
				mlc.created_at, mlc.updated_at,
				COALESCE(mlct.nickname, '') AS nickname,
				mlct.ml_user_id,
				mlc.pack_key`).
			Joins("JOIN mercadolivre_contacts mlct ON mlct.id = mlc.contact_id AND mlct.deleted_at IS NULL").
			Where("mlc.workspace_id = ?", workspaceID).
			Where("mlc.deleted_at IS NULL")
		if accountID := strings.TrimSpace(scope.ContainerID); accountID != "" {
			query = query.Where("mlc.account_id = ?", accountID)
		}
		if after != nil {
			query = query.Where("(mlc.created_at, mlc.id) < (?, ?)", after.CreatedAt, after.EntryID)
		}
		var rows []exportRow
		if err := query.Order("mlc.created_at DESC").Order("mlc.id DESC").Limit(r.pageSize).Scan(&rows).Error; err != nil {
			return err
		}
		for _, rw := range rows {
			name := strings.TrimSpace(rw.Nickname)
			if name == "" {
				name = rw.MLUserID
			}
			if err := emit(export.ChannelEntry{
				EntryID:   rw.EntryID,
				Number:    rw.PackKey,
				Name:      name,
				Status:    rw.Status,
				CreatedAt: rw.CreatedAt.Format(time.RFC3339),
				UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
			}); err != nil {
				return err
			}
		}
		if len(rows) < r.pageSize {
			return nil
		}
		last := rows[len(rows)-1]
		after = &last
	}
}
