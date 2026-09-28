package facebook_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
	fbdomain "vozko/domain/facebook"
)

type exportRepository struct {
	db *gorm.DB
}

func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

type exportRow struct {
	EntryID   string    `gorm:"column:entry_id"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
	Name      string    `gorm:"column:name"`
	FirstName string    `gorm:"column:first_name"`
	LastName  string    `gorm:"column:last_name"`
	PSID      string    `gorm:"column:psid"`
}

func (r *exportRepository) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}

	query := r.db.WithContext(ctx).
		Table("facebook_conversations fbc").
		Select(`fbc.id AS entry_id,
			COALESCE(NULLIF(fbc.conversation_status, ''), 'new') AS status,
			fbc.created_at, fbc.updated_at,
			COALESCE(fbct.name, '') AS name,
			COALESCE(fbct.first_name, '') AS first_name,
			COALESCE(fbct.last_name, '') AS last_name,
			fbct.psid`).
		Joins("JOIN facebook_contacts fbct ON fbct.id = fbc.contact_id AND fbct.deleted_at IS NULL").
		Where("fbc.workspace_id = ?", workspaceID).
		Where("fbc.deleted_at IS NULL")

	if pageID := strings.TrimSpace(scope.ContainerID); pageID != "" {
		query = query.Where("fbc.page_id = ?", pageID)
	}

	var rows []exportRow
	if err := query.Order("fbc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		contact := fbdomain.Contact{Name: rw.Name, FirstName: rw.FirstName, LastName: rw.LastName, PSID: rw.PSID}
		if err := emit(export.ChannelEntry{
			EntryID:   rw.EntryID,
			Number:    rw.PSID,
			Name:      contact.DisplayName(),
			Status:    rw.Status,
			CreatedAt: rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}
