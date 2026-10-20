package tiktok_repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

const claimOutboundSQL = `UPDATE tiktok_conversations SET outbound_since_inbound = outbound_since_inbound + 1, updated_at = now() ` +
	`WHERE id = ? AND deleted_at IS NULL AND conversation_id <> '' ` +
	`AND last_customer_message_at IS NOT NULL AND last_customer_message_at > ? ` +
	`AND outbound_since_inbound < ? ` +
	`AND (budget_closed_at IS NULL OR last_customer_message_at > budget_closed_at) ` +
	`RETURNING outbound_since_inbound`

const releaseOutboundSQL = `UPDATE tiktok_conversations SET outbound_since_inbound = GREATEST(outbound_since_inbound - 1, 0), updated_at = now() ` +
	`WHERE id = ?`

const countOutboundSQL = `UPDATE tiktok_conversations SET outbound_since_inbound = outbound_since_inbound + 1, updated_at = now() ` +
	`WHERE id = ? AND deleted_at IS NULL`

const recordInboundSQL = `UPDATE tiktok_conversations SET ` +
	`outbound_since_inbound = CASE WHEN last_customer_message_at IS NULL OR last_customer_message_at < ? THEN 0 ELSE outbound_since_inbound END, ` +
	`last_message_at = GREATEST(COALESCE(last_message_at, ?), ?), ` +
	`last_customer_message_at = GREATEST(COALESCE(last_customer_message_at, ?), ?), ` +
	`updated_at = now() ` +
	`WHERE id = ? AND deleted_at IS NULL`

const departmentForEntrySQL = `SELECT tta.department_id FROM tiktok_conversations ttc ` +
	`JOIN tiktok_accounts tta ON tta.id = ttc.account_id ` +
	`WHERE ttc.id = ? LIMIT 1`

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) ttdomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.TikTokConversation{} },
			ContainerColumn: "account_id",
			NotFound:        ttdomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.TikTokConversation{})
}

func (r *conversationRepository) FindOrCreate(ctx context.Context, in ttdomain.FindOrCreateConversationInput) (*ttdomain.Conversation, bool, error) {
	if !ttdomain.ValidOpaqueID(in.ConversationID) {
		return nil, false, ttdomain.ErrConversationIDMissing
	}
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.AccountID) == "" || strings.TrimSpace(in.ContactID) == "" {
		return nil, false, ttdomain.ErrConversationNotFound
	}
	record := &schema.TikTokConversation{
		WorkspaceID:    in.WorkspaceID,
		AccountID:      in.AccountID,
		ContactID:      in.ContactID,
		ConversationID: in.ConversationID,
		Metadata:       schema.LeadMetadata{},
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "account_id"}, {Name: "conversation_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record)
	if result.Error != nil {
		return nil, false, result.Error
	}
	conv, err := r.FindByConversationID(ctx, in.AccountID, in.ConversationID)
	if err != nil {
		return nil, false, err
	}
	return conv, result.RowsAffected == 1, nil
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*ttdomain.Conversation, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *conversationRepository) FindByConversationID(ctx context.Context, accountID, conversationID string) (*ttdomain.Conversation, error) {
	return r.first(r.db.WithContext(ctx).Where("account_id = ? AND conversation_id = ?", accountID, conversationID))
}

func (r *conversationRepository) first(query *gorm.DB) (*ttdomain.Conversation, error) {
	var record schema.TikTokConversation
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, ttdomain.ErrConversationNotFound
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByIDs(ctx context.Context, ids []string) ([]*ttdomain.Conversation, error) {
	unique := shared.UniqueNonEmpty(ids)
	out := make([]*ttdomain.Conversation, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.TikTokConversation
		if err := r.db.WithContext(ctx).Where("id IN ?", unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toConversationDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *conversationRepository) DepartmentIDForEntry(ctx context.Context, entryID string) (string, error) {
	var departmentIDs []sql.NullString
	if err := r.db.WithContext(ctx).Raw(departmentForEntrySQL, entryID).Scan(&departmentIDs).Error; err != nil {
		return "", err
	}
	if len(departmentIDs) == 0 {
		return "", ttdomain.ErrConversationNotFound
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) ClaimOutbound(ctx context.Context, id string, now time.Time) (bool, error) {
	var counts []int
	windowStart := now.Add(-ttdomain.MessagingWindow)
	if err := r.db.WithContext(ctx).Raw(claimOutboundSQL, id, windowStart, ttdomain.OutboundBudget).Scan(&counts).Error; err != nil {
		return false, err
	}
	return len(counts) == 1, nil
}

func (r *conversationRepository) ReleaseOutbound(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Exec(releaseOutboundSQL, id).Error
}

func (r *conversationRepository) CountOutbound(ctx context.Context, id string) error {
	return database.AffectedOne(r.db.WithContext(ctx).Exec(countOutboundSQL, id), ttdomain.ErrConversationNotFound)
}

func (r *conversationRepository) CloseBudget(ctx context.Context, id string, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("budget_closed_at", at), ttdomain.ErrConversationNotFound)
}

func (r *conversationRepository) RecordInbound(ctx context.Context, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return database.AffectedOne(r.db.WithContext(ctx).Exec(recordInboundSQL, at, at, at, at, at, id), ttdomain.ErrConversationNotFound)
}

func (r *conversationRepository) SetImageCapability(ctx context.Context, id string, allowed bool, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"image_send_allowed":    allowed,
		"image_send_checked_at": at,
	}), ttdomain.ErrConversationNotFound)
}

func (r *conversationRepository) SetReferral(ctx context.Context, id string, referral ttdomain.Referral) error {
	raw, err := referralJSON(referral)
	if err != nil {
		return err
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("referral", raw), ttdomain.ErrConversationNotFound)
}
