package unofficial_whatsapp_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

// containerTypeCampaign is the Scope.ContainerType value that switches this
// channel's export from "one number" to "one campaign".
const containerTypeCampaign = "campaign"

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

// ListForExport lists one container's conversations, or the whole workspace
// when none is named.
//
// The container is normally a NUMBER. When Scope.ContainerType is "campaign" it
// is a campaign instead, reached through its entry rows — which is the shape of
// this channel: a campaign entry points at a conversation rather than being one.
// ContainerType exists on Scope precisely so a channel can have more than one
// kind of container, and this is the channel that does.
//
// Scope.Statuses is applied only in the campaign case: a conversation carries a
// conversation status, while a campaign target carries a SEND status, and only
// the latter is what "leads enviados" means.
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

	query := r.db.WithContext(ctx).
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

	containerID := strings.TrimSpace(scope.ContainerID)
	byCampaign := strings.EqualFold(strings.TrimSpace(scope.ContainerType), containerTypeCampaign)

	switch {
	case byCampaign && containerID != "":
		// Through the campaign's targets. DISTINCT because two campaigns can
		// legitimately have reached the same chat, and a duplicated row in an
		// exported spreadsheet reads as a duplicated customer.
		sub := r.db.WithContext(ctx).
			Table("unofficial_whatsapp_campaign_entries uwce").
			Select("DISTINCT uwce.conversation_id").
			Where("uwce.campaign_id = ?", containerID).
			Where("uwce.conversation_id IS NOT NULL").
			Where("uwce.deleted_at IS NULL")
		if len(scope.Statuses) > 0 {
			sub = sub.Where("uwce.status IN ?", scope.Statuses)
		}
		query = query.Where("uwc.id IN (?)", sub)

	case containerID != "":
		query = query.Where("uwc.instance_id = ?", containerID)
	}

	if len(scope.DepartmentIDs) > 0 {
		// A department scope can never WIDEN what the caller may see, so it is
		// applied on the instance regardless of which container was asked for.
		query = query.Joins("JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id").
			Where("uwi.department_id IN ?", scope.DepartmentIDs)
	}

	var rows []row
	if err := query.Order("uwc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
