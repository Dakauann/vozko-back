package telegram_repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

type exportRepository struct {
	db *gorm.DB
}

// NewExportRepository builds the Telegram export source.
//
// It exists because export was structurally campaign-keyed: it demanded a
// CampaignID that a channel without campaigns cannot supply, and rejected every
// entry type but WhatsApp. A channel that customers can hold real conversations
// on but cannot get their data out of is not finished.
func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

// ListForExport lists one account's conversations, or the whole workspace when
// no account is named.
//
// Telegram has no send statuses and no campaign period, so the corresponding
// Scope fields are not applicable here and are ignored rather than faked.
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
		EntryID     string    `gorm:"column:entry_id"`
		Status      string    `gorm:"column:status"`
		CreatedAt   time.Time `gorm:"column:created_at"`
		UpdatedAt   time.Time `gorm:"column:updated_at"`
		Username    string    `gorm:"column:username"`
		FirstName   string    `gorm:"column:first_name"`
		LastName    string    `gorm:"column:last_name"`
		PhoneNumber *string   `gorm:"column:phone_number"`
		TGUserID    int64     `gorm:"column:tg_user_id"`
	}

	query := r.db.WithContext(ctx).
		Table("telegram_conversations tgc").
		Select(`tgc.id AS entry_id,
			COALESCE(NULLIF(tgc.conversation_status, ''), 'new') AS status,
			tgc.created_at, tgc.updated_at,
			COALESCE(tgcont.username, '') AS username,
			COALESCE(tgcont.first_name, '') AS first_name,
			COALESCE(tgcont.last_name, '') AS last_name,
			tgcont.phone_number,
			tgcont.tg_user_id`).
		Joins("JOIN telegram_contacts tgcont ON tgcont.id = tgc.contact_id AND tgcont.deleted_at IS NULL").
		// Tenancy is enforced here, not by the caller: an operator must not be
		// able to export another workspace's account by guessing its id.
		Where("tgc.workspace_id = ?", workspaceID).
		Where("tgc.deleted_at IS NULL")

	if accountID := strings.TrimSpace(scope.ContainerID); accountID != "" {
		query = query.Where("tgc.account_id = ?", accountID)
	}

	var rows []row
	if err := query.Order("tgc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		name := strings.TrimSpace(strings.TrimSpace(rw.FirstName) + " " + strings.TrimSpace(rw.LastName))
		if name == "" {
			name = rw.Username
		}

		// The identity slot prefers a consented phone (the only thing that links
		// a Telegram contact to the rest of the CRM), then the handle, then the
		// numeric id, so the column is never blank.
		identity := ""
		if rw.PhoneNumber != nil {
			identity = strings.TrimSpace(*rw.PhoneNumber)
		}
		if identity == "" && rw.Username != "" {
			identity = "@" + rw.Username
		}
		if identity == "" {
			identity = strconv.FormatInt(rw.TGUserID, 10)
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
