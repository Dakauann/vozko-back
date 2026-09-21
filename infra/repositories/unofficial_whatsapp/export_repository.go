package unofficial_whatsapp_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/export"
)

const containerTypeCampaign = "campaign"

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
	if strings.TrimSpace(scope.WorkspaceID) == "" {
		return nil
	}

	containerID := strings.TrimSpace(scope.ContainerID)
	if containerID != "" && strings.EqualFold(strings.TrimSpace(scope.ContainerType), containerTypeCampaign) {
		return r.listCampaignEntries(ctx, scope, containerID, emit)
	}
	return r.listConversations(ctx, scope, containerID, emit)
}

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
		Joins("LEFT JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwce.contact_id AND uwct.deleted_at IS NULL").
		Where("uwce.workspace_id = ?", scope.WorkspaceID).
		Where("uwce.campaign_id = ?", campaignID).
		Where("uwce.deleted_at IS NULL")

	if len(scope.Statuses) > 0 {
		query = query.Where("uwce.status IN ?", scope.Statuses)
	}
	if len(scope.DepartmentIDs) > 0 {
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
			EntryID:       rw.ConversationID,
			Number:        number,
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
		Where("uwc.workspace_id = ?", scope.WorkspaceID).
		Where("uwc.deleted_at IS NULL")

	if instanceID != "" {
		query = query.Where("uwc.instance_id = ?", instanceID)
	}
	if len(scope.DepartmentIDs) > 0 {
		query = query.Joins("JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id").
			Where("uwi.department_id IN ?", scope.DepartmentIDs)
	}

	var rows []row
	if err := query.Order("uwc.created_at DESC").Scan(&rows).Error; err != nil {
		return err
	}

	for _, rw := range rows {
		name := firstNonEmpty(rw.ContactName, rw.VerifiedName, rw.Name)

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
