package instagram_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
)

type conversationRepository struct {
	db *gorm.DB
}

// NewConversationRepository builds the Instagram conversation (entry) repository.
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
			// The unique index is PARTIAL (WHERE deleted_at IS NULL), because a soft-deleted
			// row must not block re-creating the same conversation. Postgres will not infer a
			// partial index as the conflict arbiter unless the predicate is repeated here,
			// a bare ON CONFLICT (cols) fails outright with 42P10 "no unique or exclusion
			// constraint matching the ON CONFLICT specification".
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

// WorkspaceIDForEntry resolves the tenant that owns a conversation.
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

// DepartmentIDForEntry reads the department from the owning account, which is the
// config carrier for its conversations.
func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentID *string
	err := r.db.WithContext(ctx).
		Table("instagram_conversations igc").
		Joins("JOIN instagram_accounts iga ON iga.id = igc.ig_account_id").
		Where("igc.id = ?", entryID).
		Limit(1).
		Pluck("iga.department_id", &departmentID).Error
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
	if err := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// RecordInbound advances the customer clock, which is the anchor for the sliding
// 24h messaging window.
//
// The update is monotonic, GREATEST against the stored value, so an
// out-of-order webhook delivery cannot move the window backwards and wrongly
// reopen or close it.
func (r *conversationRepository) RecordInbound(ctx context.Context, id string, at time.Time) error {
	return r.touchClocks(ctx, id, at, "last_customer_message_at")
}

// RecordOutbound advances the agent clock.
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

// StatusForEntry reads just the conversation status.
//
// A dedicated one-column read rather than FindByID + field access: the
// conversation-status service consults it on every status transition, and
// loading the whole row to look at one string is waste on a hot path.
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

// CountByStatus powers the inbox status chips.
//
// Conversations with no status yet count as "new", matching the inbox's own
// IS DISTINCT FROM default, otherwise brand-new conversations would be visible
// in the list but absent from every count above it.
func (r *conversationRepository) CountByStatus(ctx context.Context, workspaceID, igAccountID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	// Repeated in GROUP BY rather than referenced positionally: GORM quotes
	// Group("1") into GROUP BY "1", which Postgres rejects as a column name.
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

func (r *conversationRepository) SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramConversation{}).
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

// SetAutomationEnabled writes the per-conversation automation override.
//
// nil clears it, restoring inheritance from the account switch. Update with a
// map is required for exactly that reason: GORM's struct update skips nil
// fields, so clearing an override would silently do nothing.
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
