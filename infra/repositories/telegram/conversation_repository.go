package telegram_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	tgdomain "vozko/domain/telegram"
	"vozko/infra/database/schema"
)

type conversationRepository struct {
	db *gorm.DB
}

// NewConversationRepository builds the Telegram conversation (entry) repository.
func NewConversationRepository(db *gorm.DB) tgdomain.ConversationRepository {
	return &conversationRepository{db: db}
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
			// Partial unique index: the predicate must be repeated or Postgres
			// refuses to use it as the conflict arbiter (42P10).
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

// FindByChat resolves straight from a chat id, which is what business-mode
// deletions carry: deleted_business_messages names a chat and message ids, never
// a contact.
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

func (r *conversationRepository) WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error) {
	var workspaceID string
	err := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("id = ?", entryID).
		Limit(1).
		Pluck("workspace_id", &workspaceID).Error
	if err != nil {
		return "", err
	}
	if workspaceID == "" {
		return "", tgdomain.ErrConversationNotFound
	}
	return workspaceID, nil
}

// DepartmentIDForEntry reads the department from the owning account, which is
// the config carrier for its conversations.
func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentID *string
	err := r.db.WithContext(ctx).
		Table("telegram_conversations tgc").
		Joins("JOIN telegram_accounts tga ON tga.id = tgc.account_id").
		Where("tgc.id = ?", entryID).
		Limit(1).
		Pluck("tga.department_id", &departmentID).Error
	if err != nil {
		return "", err
	}
	if departmentID == nil {
		return "", nil
	}
	return *departmentID, nil
}

func (r *conversationRepository) ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// RecordInbound advances the customer clock.
//
// The update is monotonic — GREATEST against the stored value — so an
// out-of-order delivery cannot move the clock backwards. Telegram explicitly
// warns updates may arrive out of order, and a clock that moved backwards would
// both reorder the inbox and wrongly reopen a business-mode window.
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
	result := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_message_at": gorm.Expr("GREATEST(COALESCE(last_message_at, ?), ?)", at, at),
			sideColumn:        gorm.Expr("GREATEST(COALESCE("+sideColumn+", ?), ?)", at, at),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrConversationNotFound
	}
	return nil
}

// StatusForEntry reads just the conversation status.
//
// A dedicated one-column read rather than FindByID + field access: the
// conversation-status service consults it on every status transition, and
// loading the whole row to look at one string is waste on a hot path.
func (r *conversationRepository) StatusForEntry(ctx context.Context, id string) (string, error) {
	var status string
	err := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Where("id = ?", id).
		Limit(1).
		Pluck("COALESCE(conversation_status, '')", &status).Error
	if err != nil {
		return "", err
	}
	return status, nil
}

func (r *conversationRepository) SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
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
		return tgdomain.ErrConversationNotFound
	}
	return nil
}

// SetStartPayload records the deep-link token the contact arrived with. It is
// written once: a later /start must not overwrite the attribution that opened
// the conversation.
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

// CountByStatus powers the inbox status chips.
//
// Conversations with no status yet count as "new", matching the inbox's own
// IS DISTINCT FROM default — otherwise a channel's brand-new conversations would
// be visible in the list but absent from every count above it.
func (r *conversationRepository) CountByStatus(ctx context.Context, workspaceID, accountID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	// The status expression is repeated in GROUP BY rather than referenced
	// positionally: GORM quotes Group("1") into GROUP BY "1", which Postgres
	// reads as a column NAME and rejects with 42703.
	const statusExpr = "COALESCE(NULLIF(conversation_status, ''), 'new')"

	query := r.db.WithContext(ctx).Model(&schema.TelegramConversation{}).
		Select(statusExpr + " AS status, COUNT(*) AS cnt").
		Where("deleted_at IS NULL").
		Where("last_message_at IS NOT NULL")

	switch {
	case accountID != "":
		query = query.Where("account_id = ?", accountID)
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
