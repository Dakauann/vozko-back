package unofficial_whatsapp_repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) uw.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.UnofficialWhatsAppConversation{} },
			ContainerColumn: "instance_id",
			NotFound:        uw.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(
	ctx context.Context,
	in uw.FindOrCreateConversationInput,
) (*uw.Conversation, error) {
	campaignID := strings.TrimSpace(in.CampaignID)
	chatID := strings.TrimSpace(in.ChatID)
	if chatID != "" {
		existing, err := r.findByChat(ctx, in.InstanceID, chatID, campaignID)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, uw.ErrConversationNotFound) {
			return nil, err
		}
	}

	existing, err := r.findByContact(ctx, in.InstanceID, in.ContactID, campaignID)
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
		CampaignID:  campaignID,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}, {Name: "chat_id"}, {Name: "campaign_id"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "chat_id <> '' AND deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}

	if chatID != "" {
		return r.findByChat(ctx, in.InstanceID, chatID, campaignID)
	}
	return r.findByContact(ctx, in.InstanceID, in.ContactID, campaignID)
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
	return r.findByChat(ctx, instanceID, chatID, "")
}

func (r *conversationRepository) findByChat(ctx context.Context, instanceID, chatID, campaignID string) (*uw.Conversation, error) {
	return r.findOne(ctx, r.db.WithContext(ctx).Where("instance_id = ? AND chat_id = ?", instanceID, chatID), campaignID)
}

func (r *conversationRepository) findByContact(ctx context.Context, instanceID, contactID, campaignID string) (*uw.Conversation, error) {
	return r.findOne(ctx, r.db.WithContext(ctx).Where("instance_id = ? AND contact_id = ?", instanceID, contactID), campaignID)
}

func (r *conversationRepository) findOne(ctx context.Context, q *gorm.DB, campaignID string) (*uw.Conversation, error) {
	if campaignID != "" {
		q = q.Where("campaign_id = ?", campaignID)
	} else {
		q = q.Order("created_at DESC")
	}
	var record schema.UnofficialWhatsAppConversation
	if err := q.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_conversations uwc").
		Joins("JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id").
		Joins(conversation_repository.UnofficialCampaignJoin("uwc", "camp")).
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
	var ids []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_conversations uwc").
		Joins(conversation_repository.UnofficialCampaignJoin("uwc", "camp")).
		Where("uwc.id = ?", entryID).
		Limit(1).
		Pluck("camp.id::text", &ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 || !ids[0].Valid {
		return "", nil
	}
	return ids[0].String, nil
}
