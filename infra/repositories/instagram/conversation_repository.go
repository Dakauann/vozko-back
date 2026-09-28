package instagram_repository

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) igdomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.InstagramConversation{} },
			ContainerColumn: "ig_account_id",
			NotFound:        igdomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, workspaceID, igAccountID, contactID string) (*igdomain.Conversation, error) {
	existing, err := r.FindByContact(ctx, igAccountID, contactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, igdomain.ErrConversationNotFound) {
		return nil, err
	}

	record := &schema.InstagramConversation{
		WorkspaceID: workspaceID,
		IGAccountID: igAccountID,
		ContactID:   contactID,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_account_id"}, {Name: "contact_id"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByContact(ctx, igAccountID, contactID)
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*igdomain.Conversation, error) {
	var record schema.InstagramConversation
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByContact(ctx context.Context, igAccountID, contactID string) (*igdomain.Conversation, error) {
	var record schema.InstagramConversation
	if err := r.db.WithContext(ctx).
		First(&record, "ig_account_id = ? AND contact_id = ?", igAccountID, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("instagram_conversations igc").
		Joins("JOIN instagram_accounts iga ON iga.id = igc.ig_account_id").
		Where("igc.id = ?", entryID).
		Limit(1).
		Pluck("iga.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) SetIGConversationID(ctx context.Context, id, igConversationID string) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", id).Update("ig_conversation_id", igConversationID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrConversationNotFound
	}
	return nil
}

func toConversationDomain(record *schema.InstagramConversation) *igdomain.Conversation {
	return &igdomain.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		IGAccountID:           record.IGAccountID,
		ContactID:             record.ContactID,
		IGConversationID:      record.IGConversationID,
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
	}
}
