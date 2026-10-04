package webchat_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	wcdomain "vozko/domain/webchat"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) wcdomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.WebchatConversation{} },
			ContainerColumn: "widget_id",
			NotFound:        wcdomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, in wcdomain.FindOrCreateConversationInput) (*wcdomain.Conversation, error) {
	existing, err := r.FindByVisitor(ctx, in.WidgetID, in.VisitorID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, wcdomain.ErrConversationNotFound) {
		return nil, err
	}

	record := &schema.WebchatConversation{
		WorkspaceID: in.WorkspaceID,
		WidgetID:    in.WidgetID,
		VisitorID:   in.VisitorID,
		PageURL:     in.PageURL,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "widget_id"}, {Name: "visitor_id"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByVisitor(ctx, in.WidgetID, in.VisitorID)
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*wcdomain.Conversation, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *conversationRepository) FindByVisitor(ctx context.Context, widgetID, visitorID string) (*wcdomain.Conversation, error) {
	return r.first(ctx, "widget_id = ? AND visitor_id = ?", widgetID, visitorID)
}

func (r *conversationRepository) first(ctx context.Context, query string, args ...any) (*wcdomain.Conversation, error) {
	var record schema.WebchatConversation
	if err := r.db.WithContext(ctx).Where(query, args...).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wcdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record)
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("webchat_conversations wcc").
		Joins("JOIN webchat_widgets wcw ON wcw.id = wcc.widget_id").
		Where("wcc.id = ?", entryID).
		Limit(1).
		Pluck("wcw.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) SetPendingOptions(ctx context.Context, id string, options []wcdomain.Option) error {
	var value any
	if len(options) > 0 {
		encoded, err := json.Marshal(options)
		if err != nil {
			return err
		}
		value = datatypes.JSON(encoded)
	}
	result := r.db.WithContext(ctx).Model(&schema.WebchatConversation{}).
		Where("id = ?", id).
		Update("pending_options", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wcdomain.ErrConversationNotFound
	}
	return nil
}

func toConversationDomain(record *schema.WebchatConversation) (*wcdomain.Conversation, error) {
	var options []wcdomain.Option
	if len(record.PendingOptions) > 0 && string(record.PendingOptions) != "null" {
		if err := json.Unmarshal(record.PendingOptions, &options); err != nil {
			return nil, err
		}
	}
	return &wcdomain.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		WidgetID:              record.WidgetID,
		VisitorID:             record.VisitorID,
		ConversationStatus:    record.ConversationStatus,
		CloseSource:           record.CloseSource,
		CloseReason:           record.CloseReason,
		ClosedAt:              record.ClosedAt,
		AutomationEnabled:     record.AutomationEnabled,
		PendingOptions:        options,
		PageURL:               record.PageURL,
		LastMessageAt:         record.LastMessageAt,
		LastCustomerMessageAt: record.LastCustomerMessageAt,
		LastAgentMessageAt:    record.LastAgentMessageAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}, nil
}
