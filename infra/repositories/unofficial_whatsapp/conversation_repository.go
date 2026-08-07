package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

type conversationRepository struct {
	db *gorm.DB
}

// NewConversationRepository builds the conversation-entry repository.
func NewConversationRepository(db *gorm.DB) uw.ConversationRepository {
	return &conversationRepository{db: db}
}

func (r *conversationRepository) FindOrCreate(
	ctx context.Context,
	in uw.FindOrCreateConversationInput,
) (*uw.Conversation, error) {
	existing, err := r.findByContact(ctx, in.InstanceID, in.ContactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, uw.ErrConversationNotFound) {
		return nil, err
	}

	record := &schema.UnofficialWhatsAppConversation{
		WorkspaceID:         in.WorkspaceID,
		InstanceID:          in.InstanceID,
		ContactID:           in.ContactID,
		ChatID:              in.ChatID,
		IsGroup:             in.IsGroup,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}, {Name: "contact_id"}},
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

// FindByChatID resolves straight from a chat id, which is what the events that
// name a chat but no sender carry: a chat-level read receipt, a chat archive, a
// deletion.
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

// DepartmentIDForEntry reads the department from the owning instance, which is
// the config carrier for its conversations.
func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentID *string
	err := r.db.WithContext(ctx).
		Table("unofficial_whatsapp_conversations uwc").
		Joins("JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id").
		Where("uwc.id = ?", entryID).
		Limit(1).
		Pluck("uwi.department_id", &departmentID).Error
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
	if err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// RecordInbound advances the customer clock.
//
// Monotonic (GREATEST against the stored value) so an out-of-order delivery
// cannot move the clock backwards. This channel has no messaging window, so a
// backwards clock would not wrongly close a composer — but it WOULD reorder the
// inbox under an operator mid-triage, which is its own kind of broken.
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

// SetAutomationEnabled writes the per-conversation override.
//
// A nil value CLEARS it, so the conversation inherits the instance switch again.
// That is a genuinely different state from an explicit false — "follow the
// instance" versus "off for this one conversation" — and collapsing them would
// make an operator's takeover permanent by accident.
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

// StatusForEntry reads just the status column. A dedicated one-column read
// rather than FindByID plus field access: the conversation-status service
// consults it on every transition.
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

// CountByStatus powers the inbox status chips.
//
// Conversations with no status yet count as "new", matching the inbox's own
// IS DISTINCT FROM default. Without that, brand-new conversations are visible in
// the list but absent from every count above it, and the header reads "no work
// here" over a list full of work.
func (r *conversationRepository) CountByStatus(ctx context.Context, workspaceID, instanceID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	// The expression is repeated in GROUP BY rather than referenced
	// positionally: GORM quotes Group("1") into GROUP BY "1", which Postgres
	// reads as a column NAME and rejects with 42703.
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
