package unofficial_whatsapp_repository

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

type exportRepository struct {
	db *gorm.DB
}

// NewExportRepository builds the export source for this channel.
//
// It exists because export was structurally campaign-keyed: it demanded a
// CampaignID that a channel without campaigns cannot supply. A channel a
// customer holds real conversations on but cannot get their data out of is not
// finished — and here the data is a phone number and a name, which is exactly
// what a tenant leaving would ask for.
func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

// ListForExport lists one instance's conversations, or the whole workspace when
// no instance is named.
func (r *exportRepository) ListForExport(workspaceID, instanceID string) ([]export.ChannelEntry, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}

	type row struct {
		EntryID      string    `gorm:"column:entry_id"`
		Status       string    `gorm:"column:status"`
		CreatedAt    time.Time `gorm:"column:created_at"`
		UpdatedAt    time.Time `gorm:"column:updated_at"`
		PhoneNumber  string    `gorm:"column:phone_number"`
		JID          string    `gorm:"column:jid"`
		ContactName  string    `gorm:"column:contact_name"`
		VerifiedName string    `gorm:"column:verified_name"`
		Name         string    `gorm:"column:name"`
	}

	query := r.db.
		Table("unofficial_whatsapp_conversations uwc").
		Select(`uwc.id AS entry_id,
			COALESCE(NULLIF(uwc.conversation_status, ''), 'new') AS status,
			uwc.created_at, uwc.updated_at,
			COALESCE(uwct.phone_number, '') AS phone_number,
			COALESCE(uwct.jid, '') AS jid,
			COALESCE(uwct.contact_name, '') AS contact_name,
			COALESCE(uwct.verified_name, '') AS verified_name,
			COALESCE(uwct.name, '') AS name`).
		Joins("JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id AND uwct.deleted_at IS NULL").
		// Tenancy is enforced here, not by the caller: an operator must not be
		// able to export another workspace's number by guessing its id.
		Where("uwc.workspace_id = ?", workspaceID).
		Where("uwc.deleted_at IS NULL")

	if strings.TrimSpace(instanceID) != "" {
		query = query.Where("uwc.instance_id = ?", instanceID)
	}

	var rows []row
	if err := query.Order("uwc.created_at DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]export.ChannelEntry, 0, len(rows))
	for _, rw := range rows {
		// The same preference order the Contact entity uses for a display name,
		// so an exported spreadsheet and the inbox agree on who someone is.
		name := firstNonEmpty(rw.ContactName, rw.VerifiedName, rw.Name)

		// Unlike the other added channels, the identity slot here is a real
		// phone number: it is what the rest of the CRM keys on and what the
		// customer expects in a spreadsheet. The JID is only a fallback for a
		// contact seen under a LID whose number never resolved.
		identity := rw.PhoneNumber
		if identity == "" {
			identity = rw.JID
		} else {
			identity = "+" + identity
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
