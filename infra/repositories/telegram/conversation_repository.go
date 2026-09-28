package telegram_repository

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	tgdomain "vozko/domain/telegram"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) tgdomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.TelegramConversation{} },
			ContainerColumn: "account_id",
			NotFound:        tgdomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, in tgdomain.FindOrCreateConversationInput) (*tgdomain.Conversation, error) {
	existing, err := r.FindByContact(ctx, in.AccountID, in.ContactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, tgdomain.ErrConversationNotFound) {
		return nil, err
	}

	chatType := in.ChatType
	if chatType == "" {
		chatType = tgdomain.ChatTypePrivate
	}
	record := &schema.TelegramConversation{
		WorkspaceID:          in.WorkspaceID,
		AccountID:            in.AccountID,
		ContactID:            in.ContactID,
		TGChatID:             in.TGChatID,
		ChatType:             chatType,
		BusinessConnectionID: in.BusinessConnectionID,
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "account_id"}, {Name: "contact_id"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByContact(ctx, in.AccountID, in.ContactID)
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*tgdomain.Conversation, error) {
	var record schema.TelegramConversation
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByContact(ctx context.Context, accountID, contactID string) (*tgdomain.Conversation, error) {
	var record schema.TelegramConversation
	if err := r.db.WithContext(ctx).
		First(&record, "account_id = ? AND contact_id = ?", accountID, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByChat(ctx context.Context, accountID string, chatID int64) (*tgdomain.Conversation, error) {
	var record schema.TelegramConversation
	if err := r.db.WithContext(ctx).
		First(&record, "account_id = ? AND tg_chat_id = ?", accountID, chatID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("telegram_conversations tgc").
		Joins("JOIN telegram_accounts tga ON tga.id = tgc.account_id").
		Where("tgc.id = ?", entryID).
		Limit(1).
		Pluck("tga.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) SetStartPayload(ctx context.Context, id, payload string) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("id = ? AND start_payload IS NULL", id).
		Update("start_payload", payload)
	return result.Error
}

func (r *conversationRepository) UpdateChatID(ctx context.Context, id string, chatID int64) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("id = ?", id).Update("tg_chat_id", chatID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrConversationNotFound
	}
	return nil
}

func toConversationDomain(record *schema.TelegramConversation) *tgdomain.Conversation {
	return &tgdomain.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		AccountID:             record.AccountID,
		ContactID:             record.ContactID,
		TGChatID:              record.TGChatID,
		ChatType:              record.ChatType,
		BusinessConnectionID:  record.BusinessConnectionID,
		ConversationStatus:    record.ConversationStatus,
		CloseSource:           record.CloseSource,
		CloseReason:           record.CloseReason,
		ClosedAt:              record.ClosedAt,
		AutomationEnabled:     record.AutomationEnabled,
		LastMessageAt:         record.LastMessageAt,
		LastCustomerMessageAt: record.LastCustomerMessageAt,
		LastAgentMessageAt:    record.LastAgentMessageAt,
		StartPayload:          record.StartPayload,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}
}
