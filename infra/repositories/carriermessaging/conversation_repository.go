package carriermessaging_repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type ConversationRepository struct {
	conversation_repository.ChannelConversationTable
	db         *gorm.DB
	table      string
	containers string
	channel    carriermessaging.Channel
	newModel   func() any
}

var _ carriermessaging.ConversationRepository = (*ConversationRepository)(nil)

func NewRCSConversationRepository(db *gorm.DB) *ConversationRepository {
	return newConversationRepository(db, carriermessaging.ChannelRCS, schema.RCSConversation{}.TableName(), schema.RCSSender{}.TableName(), func() any { return &schema.RCSConversation{} })
}

func NewSMSConversationRepository(db *gorm.DB) *ConversationRepository {
	return newConversationRepository(db, carriermessaging.ChannelSMS, schema.SMSConversation{}.TableName(), schema.SMSRoute{}.TableName(), func() any { return &schema.SMSConversation{} })
}

func newConversationRepository(db *gorm.DB, channel carriermessaging.Channel, table, containers string, newModel func() any) *ConversationRepository {
	return &ConversationRepository{
		db:         db,
		table:      table,
		containers: containers,
		channel:    channel,
		newModel:   newModel,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        newModel,
			ContainerColumn: "sender_id",
			NotFound:        carriermessaging.ErrConversationNotFound,
		},
	}
}

func (r *ConversationRepository) rows(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table(r.table).Session(&gorm.Session{})
}

func (r *ConversationRepository) take(query *gorm.DB) (*carriermessaging.Conversation, error) {
	var record schema.CarrierConversation
	if err := query.Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriermessaging.ErrConversationNotFound
		}
		return nil, err
	}
	return toConversation(&record, r.channel), nil
}

func (r *ConversationRepository) findThread(ctx context.Context, workspaceID, senderID, contactID string) (*carriermessaging.Conversation, error) {
	return r.take(r.rows(ctx).Where("workspace_id = ? AND sender_id = ? AND contact_id = ?", workspaceID, senderID, contactID))
}

func (r *ConversationRepository) FindOrCreate(ctx context.Context, workspaceID, senderID, contactID string) (*carriermessaging.Conversation, bool, error) {
	existing, err := r.findThread(ctx, workspaceID, senderID, contactID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, carriermessaging.ErrConversationNotFound) {
		return nil, false, err
	}
	now := time.Now().UTC()
	result := r.rows(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "sender_id"}, {Name: "contact_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(&schema.CarrierConversation{
			ID:          newID(""),
			WorkspaceID: workspaceID,
			SenderID:    senderID,
			ContactID:   contactID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	conv, err := r.findThread(ctx, workspaceID, senderID, contactID)
	if err != nil {
		return nil, false, err
	}
	return conv, result.RowsAffected > 0, nil
}

func (r *ConversationRepository) FindByID(ctx context.Context, id string) (*carriermessaging.Conversation, error) {
	return r.take(r.rows(ctx).Where("id = ?", id))
}

func (r *ConversationRepository) FindByIDs(ctx context.Context, ids []string) ([]*carriermessaging.Conversation, error) {
	records, err := database.FindByIDs[schema.CarrierConversation](ctx, r.rows(ctx), ids)
	if err != nil {
		return nil, err
	}
	out := make([]*carriermessaging.Conversation, 0, len(records))
	for i := range records {
		out = append(out, toConversation(&records[i], r.channel))
	}
	return out, nil
}

func (r *ConversationRepository) OpenBilledConversation(ctx context.Context, id string, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(r.newModel()).
		Where("id = ? AND (conversation_opened_at IS NULL OR conversation_opened_at <= ?)", id, at.Add(-carriermessaging.ConversationWindow)).
		Update("conversation_opened_at", at)
	return changedOnce(ctx, r.db, result, r.newModel(), id, carriermessaging.ErrConversationNotFound)
}

func (r *ConversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).
		Table(r.table+" cv").
		Joins("JOIN "+r.containers+" c ON c.id = cv.sender_id").
		Where("cv.id = ?", entryID).
		Limit(1).
		Pluck("c.department_id", &departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}
