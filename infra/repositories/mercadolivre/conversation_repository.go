package mercadolivre_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
)

const claimOutboundSQL = `UPDATE mercadolivre_conversations SET outbound_since_inbound = outbound_since_inbound + 1, updated_at = now() ` +
	`WHERE id = ? AND deleted_at IS NULL AND window_open AND (outbound_budget = 0 OR outbound_since_inbound < outbound_budget) ` +
	`RETURNING outbound_since_inbound`

const releaseOutboundSQL = `UPDATE mercadolivre_conversations SET outbound_since_inbound = GREATEST(outbound_since_inbound - 1, 0), updated_at = now() ` +
	`WHERE id = ?`

const recordInboundSQL = `UPDATE mercadolivre_conversations SET ` +
	`outbound_since_inbound = CASE WHEN last_customer_message_at IS NULL OR last_customer_message_at < ? THEN 0 ELSE outbound_since_inbound END, ` +
	`last_message_at = GREATEST(COALESCE(last_message_at, ?), ?), ` +
	`last_customer_message_at = GREATEST(COALESCE(last_customer_message_at, ?), ?), ` +
	`updated_at = now() ` +
	`WHERE id = ? AND deleted_at IS NULL`

const departmentForEntrySQL = `SELECT mla.department_id FROM mercadolivre_conversations mlc ` +
	`JOIN mercadolivre_accounts mla ON mla.id = mlc.account_id ` +
	`WHERE mlc.id = ? LIMIT 1`

const maxIDsPerQuery = 1000

var errStateUnordered = errors.New("mercadolivre repository: a thread state needs the time it was read")

type conversationRepository struct {
	conversation_repository.ChannelConversationTable
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) mldomain.ConversationRepository {
	return &conversationRepository{
		db: db,
		ChannelConversationTable: conversation_repository.ChannelConversationTable{
			DB:              db,
			NewModel:        func() any { return &schema.MercadoLivreConversation{} },
			ContainerColumn: "account_id",
			NotFound:        mldomain.ErrConversationNotFound,
		},
	}
}

func (r *conversationRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.MercadoLivreConversation{})
}

func (r *conversationRepository) FindOrCreateByPack(ctx context.Context, in mldomain.FindOrCreateConversationInput) (*mldomain.Conversation, bool, error) {
	if !mldomain.ValidNumericID(in.PackKey) {
		return nil, false, mldomain.ErrPackUnknown
	}
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.AccountID) == "" || strings.TrimSpace(in.ContactID) == "" {
		return nil, false, mldomain.ErrConversationNotFound
	}
	orderIDs := in.OrderIDs
	if orderIDs == nil {
		orderIDs = []string{}
	}
	record := &schema.MercadoLivreConversation{
		WorkspaceID:   in.WorkspaceID,
		AccountID:     in.AccountID,
		ContactID:     in.ContactID,
		PackKey:       in.PackKey,
		OrderIDs:      pq.StringArray(orderIDs),
		BuyerMLUserID: in.BuyerMLUserID,
		Metadata:      schema.LeadMetadata{},
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "account_id"}, {Name: "pack_key"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record)
	if result.Error != nil {
		return nil, false, result.Error
	}
	conv, err := r.FindByPack(ctx, in.AccountID, in.PackKey)
	if err != nil {
		return nil, false, err
	}
	return conv, result.RowsAffected == 1, nil
}

func (r *conversationRepository) FindByID(ctx context.Context, id string) (*mldomain.Conversation, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *conversationRepository) FindByPack(ctx context.Context, accountID, packKey string) (*mldomain.Conversation, error) {
	return r.first(r.db.WithContext(ctx).Where("account_id = ? AND pack_key = ?", accountID, packKey))
}

func (r *conversationRepository) FindByShipment(ctx context.Context, accountID, shipmentID string) (*mldomain.Conversation, error) {
	if !mldomain.ValidNumericID(shipmentID) {
		return nil, mldomain.ErrConversationNotFound
	}
	return r.first(r.db.WithContext(ctx).Where("account_id = ? AND shipment_id = ?", accountID, shipmentID))
}

func (r *conversationRepository) first(query *gorm.DB) (*mldomain.Conversation, error) {
	var record schema.MercadoLivreConversation
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, mldomain.ErrConversationNotFound
	}
	return toConversationDomain(&record), nil
}

func (r *conversationRepository) FindByIDs(ctx context.Context, ids []string) ([]*mldomain.Conversation, error) {
	unique := shared.UniqueNonEmpty(ids)
	out := make([]*mldomain.Conversation, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.MercadoLivreConversation
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
	if len(departmentIDs) == 0 || !departmentIDs[0].Valid {
		return "", nil
	}
	return departmentIDs[0].String, nil
}

func (r *conversationRepository) ApplyStatus(ctx context.Context, id string, state mldomain.ThreadState) error {
	if state.ReadAt.IsZero() {
		return errStateUnordered
	}
	result := r.model(ctx).
		Where("id = ? AND (ml_status_read_at IS NULL OR ml_status_read_at <= ?)", id, state.ReadAt).
		Updates(map[string]any{
			"ml_status":         state.Status,
			"ml_substatus":      state.Substatus,
			"ml_status_read_at": state.ReadAt,
			"agent_mediated":    state.AgentMediated,
			"outbound_budget":   state.OutboundBudget,
			"window_open":       state.WindowOpen,
		})
	return result.Error
}

func (r *conversationRepository) ClaimOutbound(ctx context.Context, id string) (bool, error) {
	var counts []int
	if err := r.db.WithContext(ctx).Raw(claimOutboundSQL, id).Scan(&counts).Error; err != nil {
		return false, err
	}
	return len(counts) == 1, nil
}

func (r *conversationRepository) ReleaseOutbound(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Exec(releaseOutboundSQL, id).Error
}

func (r *conversationRepository) RecordInbound(ctx context.Context, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return database.AffectedOne(r.db.WithContext(ctx).Exec(recordInboundSQL, at, at, at, at, at, id), mldomain.ErrConversationNotFound)
}

func (r *conversationRepository) UpdatePackSummary(ctx context.Context, id string, summary mldomain.PackSummary, at time.Time) error {
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"pack_summary":              datatypes.JSON(raw),
		"pack_summary_refreshed_at": at,
	}
	if mldomain.ValidNumericID(summary.ShipmentID) {
		updates["shipment_id"] = summary.ShipmentID
	}
	if len(summary.OrderIDs) > 0 {
		updates["order_ids"] = pq.StringArray(summary.OrderIDs)
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(updates), mldomain.ErrConversationNotFound)
}

func (r *conversationRepository) SetBuyer(ctx context.Context, id, buyerMLUserID string) error {
	if !mldomain.ValidNumericID(buyerMLUserID) {
		return mldomain.ErrBuyerUnknown
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("buyer_ml_user_id", buyerMLUserID), mldomain.ErrConversationNotFound)
}

func toConversationDomain(record *schema.MercadoLivreConversation) *mldomain.Conversation {
	conv := &mldomain.Conversation{
		ID:                     record.ID,
		WorkspaceID:            record.WorkspaceID,
		AccountID:              record.AccountID,
		ContactID:              record.ContactID,
		PackKey:                record.PackKey,
		OrderIDs:               []string(record.OrderIDs),
		BuyerMLUserID:          record.BuyerMLUserID,
		MLStatus:               record.MLStatus,
		MLSubstatus:            record.MLSubstatus,
		MLStatusReadAt:         record.MLStatusReadAt,
		AgentMediated:          record.AgentMediated,
		OutboundBudget:         record.OutboundBudget,
		OutboundSinceInbound:   record.OutboundSinceInbound,
		WindowOpen:             record.WindowOpen,
		PackSummaryRefreshedAt: record.PackSummaryRefreshedAt,
		ConversationStatus:     record.ConversationStatus,
		CloseSource:            record.CloseSource,
		CloseReason:            record.CloseReason,
		ClosedAt:               record.ClosedAt,
		AutomationEnabled:      record.AutomationEnabled,
		LastMessageAt:          record.LastMessageAt,
		LastCustomerMessageAt:  record.LastCustomerMessageAt,
		AwaitingReplySince:     record.AwaitingReplySince,
		CreatedAt:              record.CreatedAt,
		UpdatedAt:              record.UpdatedAt,
	}
	if len(record.PackSummary) > 0 {
		var summary mldomain.PackSummary
		if err := json.Unmarshal(record.PackSummary, &summary); err == nil {
			conv.PackSummary = &summary
		}
	}
	return conv
}
