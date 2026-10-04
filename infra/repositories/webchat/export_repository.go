package webchat_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
	wcdomain "vozko/domain/webchat"
)

type exportRepository struct {
	db *gorm.DB
}

func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

func (r *exportRepository) ListForExport(
	ctx context.Context,
	scope export.Scope,
	emit func(export.ChannelEntry) error,
) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}

	type row struct {
		EntryID   string    `gorm:"column:entry_id"`
		Status    string    `gorm:"column:status"`
		CreatedAt time.Time `gorm:"column:created_at"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
		VisitorID string    `gorm:"column:visitor_id"`
		Name      string    `gorm:"column:name"`
		Email     string    `gorm:"column:email"`
		Phone     string    `gorm:"column:phone"`
	}

	query := r.db.WithContext(ctx).
		Table("webchat_conversations wcc").
		Select(`wcc.id AS entry_id,
			COALESCE(NULLIF(wcc.conversation_status, ''), 'new') AS status,
			wcc.created_at, wcc.updated_at,
			wcv.id AS visitor_id,
			COALESCE(wcv.name, '') AS name,
			COALESCE(wcv.email, '') AS email,
			COALESCE(wcv.phone, '') AS phone`).
		Joins("JOIN webchat_visitors wcv ON wcv.id = wcc.visitor_id AND wcv.deleted_at IS NULL").
		Where("wcc.workspace_id = ?", workspaceID).
		Where("wcc.deleted_at IS NULL")

	if widgetID := strings.TrimSpace(scope.ContainerID); widgetID != "" {
		query = query.Where("wcc.widget_id = ?", widgetID)
	}

	var rows []row
	if err := query.Order("wcc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		visitor := wcdomain.Visitor{ID: rw.VisitorID, Name: rw.Name, Email: rw.Email, Phone: rw.Phone}
		identity := visitor.Handle()
		if identity == "" {
			identity = rw.VisitorID
		}
		if err := emit(export.ChannelEntry{
			EntryID:   rw.EntryID,
			Number:    identity,
			Name:      visitor.DisplayName(),
			Status:    rw.Status,
			CreatedAt: rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}
