package instagram_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
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
		Username  string    `gorm:"column:username"`
		Name      string    `gorm:"column:name"`
		IGSID     string    `gorm:"column:igsid"`
	}

	query := r.db.WithContext(ctx).
		Table("instagram_conversations igc").
		Select(`igc.id AS entry_id,
			COALESCE(NULLIF(igc.conversation_status, ''), 'new') AS status,
			igc.created_at, igc.updated_at,
			COALESCE(igcont.username, '') AS username,
			COALESCE(igcont.name, '') AS name,
			igcont.igsid`).
		Joins("JOIN instagram_contacts igcont ON igcont.id = igc.contact_id AND igcont.deleted_at IS NULL").
		Where("igc.workspace_id = ?", workspaceID).
		Where("igc.deleted_at IS NULL")

	if accountID := strings.TrimSpace(scope.ContainerID); accountID != "" {
		query = query.Where("igc.ig_account_id = ?", accountID)
	}

	var rows []row
	if err := query.Order("igc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		identity := rw.IGSID
		if rw.Username != "" {
			identity = "@" + rw.Username
		}
		name := rw.Name
		if name == "" {
			name = identity
		}

		if err := emit(export.ChannelEntry{
			EntryID:   rw.EntryID,
			Number:    identity,
			Name:      name,
			Status:    rw.Status,
			CreatedAt: rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}
