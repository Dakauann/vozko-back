package emailinbox_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailinbox"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) emailinbox.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.EmailConversation{} },
			ContainerColumn: "sender_id",
			NotFound:        emailinbox.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, in emailinbox.FindOrCreateConversationInput) (*emailinbox.Conversation, bool, error) {
	existing, err := r.FindByContact(ctx, in.WorkspaceID, in.ContactID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, emailinbox.ErrConversationNotFound) {
		return nil, false, err
	}
	record := &schema.EmailConversation{
		WorkspaceID: in.WorkspaceID,
		ContactID:   in.ContactID,
		SenderID:    in.SenderID,
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "contact_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record)
	if result.Error != nil {
		return nil, false, result.Error
	}
	conv, err := r.FindByContact(ctx, in.WorkspaceID, in.ContactID)
	if err != nil {
		return nil, false, err
	}
	return conv, result.RowsAffected > 0, nil
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*emailinbox.Conversation, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *conversationRepository) FindByContact(ctx context.Context, workspaceID, contactID string) (*emailinbox.Conversation, error) {
	return r.first(ctx, "workspace_id = ? AND contact_id = ?", workspaceID, contactID)
}

func (r *conversationRepository) first(ctx context.Context, query string, args ...any) (*emailinbox.Conversation, error) {
	var record schema.EmailConversation
	if err := r.db.WithContext(ctx).Where(query, args...).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, emailinbox.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record)
}

func (r *conversationRepository) FindByIDs(ctx context.Context, ids []string) ([]*emailinbox.Conversation, error) {
	records, err := database.FindByIDs[schema.EmailConversation](ctx, r.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*emailinbox.Conversation, 0, len(records))
	for i := range records {
		conv, err := toConversationDomain(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, conv)
	}
	return out, nil
}

func (r *conversationRepository) ApplyThread(ctx context.Context, id string, thread emailinbox.Thread) error {
	chain, err := json.Marshal(thread.References)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.EmailConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"subject":                 thread.Subject,
			"thread_key":              thread.ThreadKey,
			"last_inbound_message_id": thread.LastInboundMessageID,
			"reference_chain":         datatypes.JSON(chain),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return emailinbox.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("email_conversations emc").
		Joins("JOIN email_inboxes emi ON emi.id = emc.sender_id").
		Where("emc.id = ?", entryID).
		Limit(1).
		Pluck("emi.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func toConversationDomain(record *schema.EmailConversation) (*emailinbox.Conversation, error) {
	var references []string
	if len(record.References) > 0 && string(record.References) != "null" {
		if err := json.Unmarshal(record.References, &references); err != nil {
			return nil, err
		}
	}
	return &emailinbox.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		ContactID:             record.ContactID,
		SenderID:              record.SenderID,
		Subject:               record.Subject,
		ThreadKey:             record.ThreadKey,
		LastInboundMessageID:  record.LastInboundMessageID,
		References:            references,
		ConversationStatus:    record.ConversationStatus,
		CloseSource:           record.CloseSource,
		CloseReason:           record.CloseReason,
		ClosedAt:              record.ClosedAt,
		AutomationEnabled:     record.AutomationEnabled,
		LastMessageAt:         record.LastMessageAt,
		LastCustomerMessageAt: record.LastCustomerMessageAt,
		LastAgentMessageAt:    record.LastAgentMessageAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}, nil
}
