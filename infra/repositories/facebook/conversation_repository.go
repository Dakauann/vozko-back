package facebook_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) fbdomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.FacebookConversation{} },
			ContainerColumn: "page_id",
			NotFound:        fbdomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, workspaceID, pageID, contactID string) (*fbdomain.Conversation, error) {
	existing, err := r.FindByContact(ctx, pageID, contactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, fbdomain.ErrConversationNotFound) {
		return nil, err
	}
	record := &schema.FacebookConversation{WorkspaceID: workspaceID, PageID: pageID, ContactID: contactID}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "page_id"}, {Name: "contact_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByContact(ctx, pageID, contactID)
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*fbdomain.Conversation, error) {
	var record schema.FacebookConversation
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByContact(ctx context.Context, pageID, contactID string) (*fbdomain.Conversation, error) {
	var record schema.FacebookConversation
	if err := r.db.WithContext(ctx).First(&record, "page_id = ? AND contact_id = ?", pageID, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) LatestForPage(ctx context.Context, pageID string) (*fbdomain.Conversation, error) {
	var record schema.FacebookConversation
	if err := r.db.WithContext(ctx).
		Where("page_id = ?", pageID).
		Order("last_customer_message_at DESC NULLS LAST").
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table("facebook_conversations fbc").
		Joins("JOIN facebook_pages fbp ON fbp.id = fbc.page_id").
		Where("fbc.id = ?", entryID).
		Limit(1).
		Pluck("fbp.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) AdvanceWatermark(ctx context.Context, id string, kind fbdomain.WatermarkKind, at time.Time) error {
	column := "delivered_watermark"
	if kind == fbdomain.WatermarkRead {
		column = "read_watermark"
	}
	result := r.db.WithContext(ctx).Model(&schema.FacebookConversation{}).
		Where("id = ?", id).
		Update(column, gorm.Expr("GREATEST(COALESCE("+column+", ?), ?)", at, at))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) SetThreadOwner(ctx context.Context, id, appID string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.FacebookConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{"thread_owner_app_id": appID, "thread_owner_seen_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) MergeMetadata(ctx context.Context, id string, values map[string]any) error {
	return r.writeMetadata(ctx, id, values, "COALESCE(metadata, '{}'::jsonb) || ?::jsonb")
}

func (r *conversationRepository) SeedMetadata(ctx context.Context, id string, values map[string]any) error {
	return r.writeMetadata(ctx, id, values, "?::jsonb || COALESCE(metadata, '{}'::jsonb)")
}

func (r *conversationRepository) writeMetadata(ctx context.Context, id string, values map[string]any, expr string) error {
	if len(values) == 0 {
		return nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.FacebookConversation{}).
		Where("id = ?", id).
		Update("metadata", gorm.Expr(expr, string(payload)))

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrConversationNotFound
	}
	return nil
}

func (r *conversationRepository) SetFBConversationID(ctx context.Context, id, fbConversationID string) error {
	result := r.db.WithContext(ctx).Model(&schema.FacebookConversation{}).
		Where("id = ?", id).Update("fb_conversation_id", fbConversationID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrConversationNotFound
	}
	return nil
}

func toConversationDomain(record *schema.FacebookConversation) *fbdomain.Conversation {
	return &fbdomain.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		PageID:                record.PageID,
		ContactID:             record.ContactID,
		FBConversationID:      record.FBConversationID,
		ConversationStatus:    record.ConversationStatus,
		CloseSource:           record.CloseSource,
		CloseReason:           record.CloseReason,
		ClosedAt:              record.ClosedAt,
		AutomationEnabled:     record.AutomationEnabled,
		LastMessageAt:         record.LastMessageAt,
		LastCustomerMessageAt: record.LastCustomerMessageAt,
		LastAgentMessageAt:    record.LastAgentMessageAt,
		DeliveredWatermark:    record.DeliveredWatermark,
		ReadWatermark:         record.ReadWatermark,
		ThreadOwnerAppID:      record.ThreadOwnerAppID,
		ThreadOwnerSeenAt:     record.ThreadOwnerSeenAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}
}
