package emailinbox_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	"vozko/domain/emailinbox"
	"vozko/domain/export"
)

type exportRepository struct {
	db *gorm.DB
}

func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

func (r *exportRepository) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}

	type row struct {
		EntryID     string    `gorm:"column:entry_id"`
		Status      string    `gorm:"column:status"`
		CreatedAt   time.Time `gorm:"column:created_at"`
		UpdatedAt   time.Time `gorm:"column:updated_at"`
		Address     string    `gorm:"column:address"`
		DisplayName string    `gorm:"column:display_name"`
	}

	query := r.db.WithContext(ctx).
		Table("email_conversations emc").
		Select(`emc.id AS entry_id,
			COALESCE(NULLIF(emc.conversation_status, ''), 'new') AS status,
			emc.created_at, emc.updated_at,
			ect.address AS address,
			COALESCE(ect.display_name, '') AS display_name`).
		Joins("JOIN email_contacts ect ON ect.id = emc.contact_id AND ect.deleted_at IS NULL").
		Where("emc.workspace_id = ?", workspaceID).
		Where("emc.deleted_at IS NULL")

	if senderID := strings.TrimSpace(scope.ContainerID); senderID != "" {
		query = query.Where("emc.sender_id = ?", senderID)
	}

	var rows []row
	if err := query.Order("emc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}
	for _, rw := range rows {
		contact := emailinbox.Contact{Address: emailaddress.Address(rw.Address), DisplayName: rw.DisplayName}
		if err := emit(export.ChannelEntry{
			EntryID:   rw.EntryID,
			Number:    rw.Address,
			Name:      contact.Name(),
			Status:    rw.Status,
			CreatedAt: rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}
