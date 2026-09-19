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
// The container is normally a NUMBER, and then a row is a conversation. When
// Scope.ContainerType is "campaign" it is a campaign instead, and then a row is
// a campaign ENTRY - see listCampaignEntries for why the two cannot be the same
// query. ContainerType exists on Scope precisely so a channel can have more
// than one kind of container, and this is the channel that does.
func (r *exportRepository) ListForExport(
	ctx context.Context,
	scope export.Scope,
	emit func(export.ChannelEntry) error,
) error {
	if strings.TrimSpace(scope.WorkspaceID) == "" {
		return nil
	}

	containerID := strings.TrimSpace(scope.ContainerID)
	if containerID != "" && strings.EqualFold(strings.TrimSpace(scope.ContainerType), containerTypeCampaign) {
		return r.listCampaignEntries(ctx, scope, containerID, emit)
	}
	return r.listConversations(ctx, scope, containerID, emit)
}

// listCampaignEntries walks a campaign's TARGETS, not the chats it produced.
//
// It used to walk conversations reached through the entries, and that made the
// export unable to answer the two questions an operator opens it for. A target
// only gets a conversation_id once its send SUCCEEDS, so every failure, every
// number not on WhatsApp and everything still pending was missing from the file
// entirely - and the status column carried the CONVERSATION status ("new",
// "open") while the status filter was matching SEND statuses, so asking for the
// failed ones returned nothing at all.
//
// EntryID stays the conversation id because that is what this channel keys its
// analyses and stages on. A target that never reached a chat has none, and its
// analysis columns are simply blank, which is the truth about it.
func (r *exportRepository) listCampaignEntries(
	ctx context.Context,
	scope export.Scope,
	campaignID string,
	emit func(export.ChannelEntry) error,
) error {
	type row struct {
		ConversationID string    `gorm:"column:conversation_id"`
		Status         string    `gorm:"column:status"`
		ErrorCode      int       `gorm:"column:error_code"`
		ErrorMessage   string    `gorm:"column:error_message"`
		CreatedAt      time.Time `gorm:"column:created_at"`
		UpdatedAt      time.Time `gorm:"column:updated_at"`
		Number         string    `gorm:"column:number"`
		EntryName      string    `gorm:"column:entry_name"`
		ContactName    string    `gorm:"column:contact_name"`
		VerifiedName   string    `gorm:"column:verified_name"`
		ProfileName    string    `gorm:"column:profile_name"`
	}

	query := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_campaign_entries uwce").
		Select(`COALESCE(uwce.conversation_id::text, '') AS conversation_id,
			uwce.status,
			uwce.error_code,
			COALESCE(uwce.error_message, '') AS error_message,
			uwce.created_at, uwce.updated_at,
			COALESCE(uwce.number, '') AS number,
			COALESCE(uwce.name, '') AS entry_name,
			COALESCE(uwct.contact_name, '') AS contact_name,
			COALESCE(uwct.verified_name, '') AS verified_name,
			COALESCE(uwct.name, '') AS profile_name`).
		Joins("JOIN unofficial_whatsapp_campaigns uwcp ON uwcp.id = uwce.campaign_id AND uwcp.deleted_at IS NULL").
		// The contact is only known once a send resolved one, so this join must
		// not drop the rows that failed before it.
		Joins("LEFT JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwce.contact_id AND uwct.deleted_at IS NULL").
		// Tenancy is enforced here, not by the caller: an operator must not be
		// able to export another workspace's campaign by guessing its id.
		Where("uwce.workspace_id = ?", scope.WorkspaceID).
		Where("uwce.campaign_id = ?", campaignID).
		Where("uwce.deleted_at IS NULL")

	if len(scope.Statuses) > 0 {
		query = query.Where("uwce.status IN ?", scope.Statuses)
	}
	if len(scope.DepartmentIDs) > 0 {
		// On the CAMPAIGN's department, the same column the campaign list
		// scopes by. An export must not reach what the list hides.
		query = query.Where("uwcp.department_id IN ?", scope.DepartmentIDs)
	}

	var rows []row
	if err := query.Order("uwce.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		number := rw.Number
		if number != "" {
			number = "+" + number
		}

		if err := emit(export.ChannelEntry{
			EntryID: rw.ConversationID,
			Number:  number,
			// The contact's own names first, the imported one last: a target
			// that never reached WhatsApp only ever has the imported name.
			Name:          firstNonEmpty(rw.ContactName, rw.VerifiedName, rw.ProfileName, rw.EntryName),
			Status:        rw.Status,
			FailureCode:   rw.ErrorCode,
			FailureReason: rw.ErrorMessage,
			CreatedAt:     rw.CreatedAt.Format(time.RFC3339),
			UpdatedAt:     rw.UpdatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return nil
}

// listConversations walks the chats on one number, or on every number in the
// workspace.
//
// Scope.Statuses is deliberately NOT applied here: a conversation carries a
// conversation status, while the filter that reaches this port speaks send
// statuses, and matching one against the other would silently empty the file.
func (r *exportRepository) listConversations(
	ctx context.Context,
	scope export.Scope,
	instanceID string,
	emit func(export.ChannelEntry) error,
) error {
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
		Where("uwc.workspace_id = ?", scope.WorkspaceID).
		Where("uwc.deleted_at IS NULL")

	if instanceID != "" {
		query = query.Where("uwc.instance_id = ?", instanceID)
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
