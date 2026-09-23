package instagram_repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

type conversationRepository struct {
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) igdomain.ConversationRepository {
	return &conversationRepository{db: db}
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

func (r *conversationRepository) WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error) {
	var workspaceID string
	err := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", entryID).
		Limit(1).
		Pluck("workspace_id", &workspaceID).Error
	if err != nil {
		return "", err
	}
	if workspaceID == "" {
		return "", igdomain.ErrConversationNotFound
	}
	return workspaceID, nil
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

func (r *conversationRepository) ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
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
	result := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_message_at": gorm.Expr("GREATEST(COALESCE(last_message_at, ?), ?)", at, at),
			sideColumn:        gorm.Expr("GREATEST(COALESCE("+sideColumn+", ?), ?)", at, at),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrConversationNotFound
	}
	return nil
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

func (r *conversationRepository) StatusForEntry(ctx context.Context, id string) (string, error) {
	var status string
	err := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", id).
		Limit(1).
		Pluck("COALESCE(conversation_status, '')", &status).Error
	if err != nil {
		return "", err
	}
	return status, nil
}

func (r *conversationRepository) CountByStatus(ctx context.Context, workspaceID, igAccountID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	const statusExpr = "COALESCE(NULLIF(conversation_status, ''), 'new')"

	query := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Select(statusExpr + " AS status, COUNT(*) AS cnt").
		Where("deleted_at IS NULL").
		Where("last_message_at IS NOT NULL")

	switch {
	case igAccountID != "":
		query = query.Where("ig_account_id = ?", igAccountID)
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
	for _, rw := range rows {
		out[rw.Status] = rw.Count
	}
	return out, nil
}

func (r *conversationRepository) SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", id).
		Updates(conversation_repository.StatusUpdates(write))
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

func (r *conversationRepository) SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("id = ?", id).
		Update("automation_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrConversationNotFound
	}
	return nil
}
