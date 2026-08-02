package instagram_repository

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

type exportRepository struct {
	db *gorm.DB
}

// NewExportRepository builds the Instagram export source.
//
// Export was WhatsApp-only and structurally campaign-keyed, so an Instagram
// tenant had no way to get their conversation data out at all. The account is
// the container here, exactly as it is everywhere else on this channel.
func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

// ListForExport lists one account's conversations, or the whole workspace when
// no account is named.
func (r *exportRepository) ListForExport(workspaceID, accountID string) ([]export.ChannelEntry, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, nil
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

	query := r.db.
		Table("instagram_conversations igc").
		Select(`igc.id AS entry_id,
			COALESCE(NULLIF(igc.conversation_status, ''), 'new') AS status,
			igc.created_at, igc.updated_at,
			COALESCE(igcont.username, '') AS username,
			COALESCE(igcont.name, '') AS name,
			igcont.igsid`).
		Joins("JOIN instagram_contacts igcont ON igcont.id = igc.contact_id AND igcont.deleted_at IS NULL").
		// Tenancy is enforced here, not by the caller.
		Where("igc.workspace_id = ?", workspaceID).
		Where("igc.deleted_at IS NULL")

	if strings.TrimSpace(accountID) != "" {
		query = query.Where("igc.ig_account_id = ?", accountID)
	}

	var rows []row
	if err := query.Order("igc.created_at DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]export.ChannelEntry, 0, len(rows))
	for _, rw := range rows {
		// An Instagram contact has no phone number, so the handle fills the
		// identity slot — that is what an operator recognises.
		identity := rw.IGSID
		if rw.Username != "" {
			identity = "@" + rw.Username
		}
		name := rw.Name
		if name == "" {
			name = identity
		}

		out = append(out, export.ChannelEntry{
			EntryID:   rw.EntryID,
			Number:    identity,
			Name:      name,
			Status:    rw.Status,
			CreatedAt: rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt: rw.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}
