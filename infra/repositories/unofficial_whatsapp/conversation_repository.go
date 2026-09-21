package unofficial_whatsapp_repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

type conversationRepository struct {
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) uw.ConversationRepository {
	return &conversationRepository{db: db}
}

func (r *conversationRepository) FindOrCreate(
	ctx context.Context,
	in uw.FindOrCreateConversationInput,
) (*uw.Conversation, error) {
	if chatID := strings.TrimSpace(in.ChatID); chatID != "" {
		existing, err := r.FindByChatID(ctx, in.InstanceID, chatID)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, uw.ErrConversationNotFound) {
			return nil, err
		}
	}

	existing, err := r.findByContact(ctx, in.InstanceID, in.ContactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, uw.ErrConversationNotFound) {
		return nil, err
	}

	record := &schema.UnofficialWhatsAppConversation{
		WorkspaceID: in.WorkspaceID,
		InstanceID:  in.InstanceID,
		ContactID:   in.ContactID,
		ChatID:      in.ChatID,
		IsGroup:     in.IsGroup,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}, {Name: "chat_id"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "chat_id <> '' AND deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}

	if chatID := strings.TrimSpace(in.ChatID); chatID != "" {
		return r.FindByChatID(ctx, in.InstanceID, chatID)
	}
	return r.findByContact(ctx, in.InstanceID, in.ContactID)
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*uw.Conversation, error) {
	var record schema.UnofficialWhatsAppConversation
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByChatID(ctx context.Context, instanceID, chatID string) (*uw.Conversation, error) {
	var record schema.UnofficialWhatsAppConversation
	err := r.db.WithContext(ctx).
		First(&record, "instance_id = ? AND chat_id = ?", instanceID, chatID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) findByContact(ctx context.Context, instanceID, contactID string) (*uw.Conversation, error) {
	var record schema.UnofficialWhatsAppConversation
	err := r.db.WithContext(ctx).
		First(&record, "instance_id = ? AND contact_id = ?", instanceID, contactID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error) {
	var workspaceID string
	err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("id = ?", entryID).
		Limit(1).
		Pluck("workspace_id", &workspaceID).Error
	if err != nil {
		return "", err
	}
	if workspaceID == "" {
		return "", uw.ErrConversationNotFound
	}
	return workspaceID, nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_conversations uwc").
		Joins("JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id").
		Joins(`LEFT JOIN LATERAL (
			SELECT uwcamp.department_id
			FROM unofficial_whatsapp_campaign_entries uwce
			JOIN unofficial_whatsapp_campaigns uwcamp
			  ON uwcamp.id = uwce.campaign_id AND uwcamp.deleted_at IS NULL
			WHERE uwce.conversation_id = uwc.id
			  AND uwce.deleted_at IS NULL
			  AND uwcamp.department_id IS NOT NULL
			ORDER BY uwce.sent_at DESC NULLS LAST, uwce.updated_at DESC
			LIMIT 1
		) camp ON TRUE`).
		Where("uwc.id = ?", entryID).
		Limit(1).
		Pluck("COALESCE(camp.department_id, uwi.department_id)", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) CampaignIDForEntry(ctx context.Context, entryID string) (string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_campaign_entries uwce").
		Joins("JOIN unofficial_whatsapp_campaigns uwcamp ON uwcamp.id = uwce.campaign_id AND uwcamp.deleted_at IS NULL").
		Where("uwce.conversation_id = ? AND uwce.deleted_at IS NULL", entryID).
		Order("uwce.sent_at DESC NULLS LAST, uwce.updated_at DESC").
		Limit(1).
		Pluck("uwce.campaign_id::text", &ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

func (r *conversationRepository) ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *conversationRepository) RecordInbound(ctx context.Context, id string, at time.Time) error {
	return r.touchClocks(ctx, id, at, "last_customer_message_at")
}

func (r *conversationRepository) RecordOutbound(ctx context.Context, id string, at time.Time) error {
	return r.touchClocks(ctx, id, at, "last_agent_message_at")
}

func (r *conversationRepository) touchClocks(ctx context.Context, id string, at time.Time, sideColumn string) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_message_at": gorm.Expr("GREATEST(COALESCE(last_message_at, ?), ?)", at, at),
			sideColumn:        gorm.Expr("GREATEST(COALESCE("+sideColumn+", ?), ?)", at, at),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"conversation_status": status,
			"close_source":        closeSource,
			"close_reason":        closeReason,
			"closed_at":           closedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("id = ?", id).
		Update("automation_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) StatusForEntry(ctx context.Context, id string) (string, error) {
	var status string
	err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("id = ?", id).
		Limit(1).
		Pluck("COALESCE(conversation_status, '')", &status).Error
	if err != nil {
		return "", err
	}
	return status, nil
}

func (r *conversationRepository) CountByStatus(ctx context.Context, workspaceID, instanceID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	const statusExpr = "COALESCE(NULLIF(conversation_status, ''), 'new')"

	query := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Select(statusExpr + " AS status, COUNT(*) AS cnt").
		Where("deleted_at IS NULL").
		Where("last_message_at IS NOT NULL")

	switch {
	case instanceID != "":
		query = query.Where("instance_id = ?", instanceID)
	case workspaceID != "":
		query = query.Where("workspace_id = ?", workspaceID)
	default:
		return map[string]int64{}, nil
	}

	var rows []row
	if err := query.Group(statusExpr).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[string]int64, len(rows))
	for _, item := range rows {
		out[item.Status] = item.Count
	}
	return out, nil
}
