package tiktok_repository

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
	if db == nil {
		panic("tiktok export: the database is required")
	}
	return &exportRepository{db: db, pageSize: exportPageSize}
}

type exportRow struct {
	EntryID          string    `gorm:"column:entry_id"`
	Status           string    `gorm:"column:status"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
	DisplayName      string    `gorm:"column:display_name"`
	Username         string    `gorm:"column:username"`
	UniqueIdentifier string    `gorm:"column:unique_identifier"`
}

func (rw exportRow) entry() export.ChannelEntry {
	handle := strings.TrimSpace(rw.Username)
	number := rw.UniqueIdentifier
	if handle != "" {
		number = "@" + handle
	}
	name := strings.TrimSpace(rw.DisplayName)
	if name == "" {
		name = handle
	}
	if name == "" {
		name = rw.UniqueIdentifier
	}
	return export.ChannelEntry{
		EntryID:   rw.EntryID,
		Number:    number,
		Name:      name,
		Status:    rw.Status,
		CreatedAt: rw.CreatedAt.Format(time.RFC3339),
		UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
	}
}

func (r *exportRepository) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}
	var after *exportRow
	for {
		query := r.db.WithContext(ctx).
			Table("tiktok_conversations ttc").
			Select(`ttc.id AS entry_id,
				COALESCE(NULLIF(ttc.conversation_status, ''), 'new') AS status,
				ttc.created_at, ttc.updated_at,
				ttct.display_name, ttct.username, ttct.unique_identifier`).
			Joins("JOIN tiktok_contacts ttct ON ttct.id = ttc.contact_id AND ttct.deleted_at IS NULL").
			Where("ttc.workspace_id = ?", workspaceID).
			Where("ttc.deleted_at IS NULL")
		if accountID := strings.TrimSpace(scope.ContainerID); accountID != "" {
			query = query.Where("ttc.account_id = ?", accountID)
		}
		if after != nil {
			query = query.Where("(ttc.created_at, ttc.id) < (?, ?)", after.CreatedAt, after.EntryID)
		}
		var rows []exportRow
		if err := query.Order("ttc.created_at DESC").Order("ttc.id DESC").Limit(r.pageSize).Scan(&rows).Error; err != nil {
			return err
		}
		for _, rw := range rows {
			if err := emit(rw.entry()); err != nil {
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
