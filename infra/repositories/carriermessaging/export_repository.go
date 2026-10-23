package carriermessaging_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
	"vozko/domain/export"
	"vozko/infra/database/schema"
)

type exportRepository struct {
	db    *gorm.DB
	table string
}

func NewExportRepository(db *gorm.DB, channel carriermessaging.Channel) export.ChannelEntryLister {
	table := schema.RCSConversation{}.TableName()
	if channel == carriermessaging.ChannelSMS {
		table = schema.SMSConversation{}.TableName()
	}
	return &exportRepository{db: db, table: table}
}

func (r *exportRepository) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}
	var rows []struct {
		EntryID     string    `gorm:"column:entry_id"`
		Status      string    `gorm:"column:status"`
		CreatedAt   time.Time `gorm:"column:created_at"`
		UpdatedAt   time.Time `gorm:"column:updated_at"`
		Phone       string    `gorm:"column:phone"`
		DisplayName string    `gorm:"column:display_name"`
	}
	query := r.db.WithContext(ctx).
		Table(r.table+" cv").
		Select(`cv.id AS entry_id,
			COALESCE(NULLIF(cv.conversation_status, ''), 'new') AS status,
			cv.created_at, cv.updated_at, ct.phone, ct.display_name`).
		Joins("JOIN carrier_contacts ct ON ct.id = cv.contact_id AND ct.deleted_at IS NULL").
		Where("cv.workspace_id = ? AND cv.deleted_at IS NULL", workspaceID)
	if senderID := strings.TrimSpace(scope.ContainerID); senderID != "" {
		query = query.Where("cv.sender_id = ?", senderID)
	}
	if err := query.Order("cv.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		contact := carriermessaging.Contact{Phone: row.Phone, DisplayName: row.DisplayName}
		if err := emit(export.ChannelEntry{
			EntryID:   row.EntryID,
			Number:    row.Phone,
			Name:      contact.Name(),
			Status:    row.Status,
			CreatedAt: row.CreatedAt.Format(time.RFC3339),
			UpdatedAt: row.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}
