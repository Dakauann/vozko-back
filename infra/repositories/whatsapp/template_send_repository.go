package whatsapp_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
)

// templateSendRepository stores paid-send attempts.
//
// Every write here is a conditional write. That is not defensive style: the
// callers are several replicas racing on one row, and a read-then-write between
// them is a duplicate charge. Postgres arbitrates; this file never does.
type templateSendRepository struct {
	db *gorm.DB
}

func NewTemplateSendRepository(db *gorm.DB) template.SendAttemptRepository {
	return &templateSendRepository{db: db}
}

var _ template.SendAttemptRepository = (*templateSendRepository)(nil)

// sendAttemptConflictClause is the insert's collision rule, and it is a matched
// pair with ux_wa_tpl_send_idem in infra/database/indexes.go.
//
// TargetWhere repeats that index's predicate, which is NOT decoration. The index
// is PARTIAL, and Postgres only infers a partial index for ON CONFLICT when the
// statement carries a predicate implying the index's own. Without it the server
// finds no unique constraint covering (workspace_id, idempotency_key) and
// rejects the insert with 42P10 — so the FIRST send fails outright, and the
// idempotency this table exists to provide never engages, because no row is ever
// written for the winners and losers to arbitrate on.
//
// Change the index predicate and you must change this with it.
func sendAttemptConflictClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "workspace_id"}, {Name: "idempotency_key"}},
		TargetWhere: clause.Where{
			Exprs: []clause.Expression{
				clause.Expr{SQL: "idempotency_key IS NOT NULL AND deleted_at IS NULL"},
			},
		},
		// DoNothing, never DoUpdates: the loser of the race must not overwrite the
		// winner's row, which may already be charged.
		DoNothing: true,
	}
}

// CreateIfAbsent is the gate the whole exactly-once argument stands on.
//
// ON CONFLICT DO NOTHING plus RowsAffected is what distinguishes the one caller
// that may spend money from every other caller of the same request. Doing this
// as SELECT-then-INSERT would reintroduce exactly the race the unique index
// exists to close.
func (r *templateSendRepository) CreateIfAbsent(ctx context.Context, attempt *template.SendAttempt) (*template.SendAttempt, bool, error) {
	if attempt == nil {
		return nil, false, errors.New("whatsapp template send: attempt is required")
	}
	if strings.TrimSpace(attempt.WorkspaceID) == "" {
		return nil, false, template.ErrWorkspaceRequired
	}
	if strings.TrimSpace(attempt.IdempotencyKey) == "" {
		return nil, false, template.ErrIdempotencyKeyRequired
	}

	row := toSendSchema(attempt)
	result := r.db.WithContext(ctx).
		Clauses(sendAttemptConflictClause()).
		Create(&row)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return toSendDomain(&row), true, nil
	}

	// Somebody else won. Their row is the answer to this request — returning a
	// fresh one would let the loser act as though it had created something.
	stored, err := r.FindByIdempotencyKey(ctx, attempt.WorkspaceID, attempt.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	return stored, false, nil
}

func (r *templateSendRepository) FindByID(ctx context.Context, id string) (*template.SendAttempt, error) {
	var row schema.WhatsAppTemplateSend
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSendDomain(&row), nil
}

func (r *templateSendRepository) FindByIdempotencyKey(ctx context.Context, workspaceID, key string) (*template.SendAttempt, error) {
	var row schema.WhatsAppTemplateSend
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND idempotency_key = ?", workspaceID, key).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSendDomain(&row), nil
}

// FindByProviderMessageID is scoped to the workspace even though a wamid is
// globally unique: an unscoped lookup would let one tenant's webhook resolve
// another tenant's attempt if a message id were ever replayed at us.
func (r *templateSendRepository) FindByProviderMessageID(ctx context.Context, workspaceID, providerMessageID string) (*template.SendAttempt, error) {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" {
		return nil, nil
	}
	query := r.db.WithContext(ctx).Where("provider_message_id = ?", providerMessageID)
	if strings.TrimSpace(workspaceID) != "" {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	var row schema.WhatsAppTemplateSend
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSendDomain(&row), nil
}

// transitionTo is the shared conditional write. Legal predecessors come from the
// domain's state machine rather than being restated here, so the database and
// the entity cannot drift into disagreeing about what is allowed.
func (r *templateSendRepository) transitionTo(ctx context.Context, id string, next template.SendAttemptStatus, updates map[string]interface{}) error {
	from := template.PredecessorsOf(next)
	if len(from) == 0 {
		return template.ErrSendAttemptConflict
	}
	allowed := make([]string, 0, len(from))
	for _, s := range from {
		allowed = append(allowed, string(s))
	}

	updates["status"] = string(next)
	result := r.db.WithContext(ctx).
		Model(&schema.WhatsAppTemplateSend{}).
		Where("id = ? AND status IN ? AND deleted_at IS NULL", id, allowed).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrSendAttemptConflict
	}
	return nil
}

func (r *templateSendRepository) MarkCharged(ctx context.Context, id string, chargedMicros int64, at time.Time) error {
	return r.transitionTo(ctx, id, template.SendAttemptCharged, map[string]interface{}{
		"charged_micros": chargedMicros,
		"charged_at":     at.UTC(),
	})
}

func (r *templateSendRepository) MarkSent(ctx context.Context, id string, providerMessageID string, responseStatus int, at time.Time) error {
	return r.transitionTo(ctx, id, template.SendAttemptSent, map[string]interface{}{
		"provider_message_id": strings.TrimSpace(providerMessageID),
		"response_status":     responseStatus,
		"sent_at":             at.UTC(),
	})
}

func (r *templateSendRepository) MarkRejected(ctx context.Context, id string, errorCode int, errorMessage string, responseStatus int) error {
	if len(errorMessage) > 500 {
		errorMessage = errorMessage[:500]
	}
	return r.transitionTo(ctx, id, template.SendAttemptRejected, map[string]interface{}{
		"error_code":      errorCode,
		"error_message":   errorMessage,
		"response_status": responseStatus,
	})
}

func (r *templateSendRepository) MarkUnknown(ctx context.Context, id string, errorMessage string, responseStatus int) error {
	if len(errorMessage) > 500 {
		errorMessage = errorMessage[:500]
	}
	return r.transitionTo(ctx, id, template.SendAttemptUnknown, map[string]interface{}{
		"error_message":   errorMessage,
		"response_status": responseStatus,
	})
}

func (r *templateSendRepository) MarkRefunded(ctx context.Context, id string, at time.Time) error {
	return r.transitionTo(ctx, id, template.SendAttemptRefunded, map[string]interface{}{
		"refunded_at": at.UTC(),
	})
}

// ListNeedingReconciliation returns attempts that took money and never settled.
// Oldest first: the longest-held money is refunded first.
func (r *templateSendRepository) ListNeedingReconciliation(ctx context.Context, olderThan time.Time, limit int) ([]*template.SendAttempt, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []schema.WhatsAppTemplateSend
	err := r.db.WithContext(ctx).
		Where("status IN ? AND updated_at < ? AND deleted_at IS NULL",
			[]string{string(template.SendAttemptCharged), string(template.SendAttemptUnknown)},
			olderThan.UTC()).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*template.SendAttempt, 0, len(rows))
	for i := range rows {
		out = append(out, toSendDomain(&rows[i]))
	}
	return out, nil
}

func toSendSchema(a *template.SendAttempt) schema.WhatsAppTemplateSend {
	return schema.WhatsAppTemplateSend{
		ID:                a.ID,
		WorkspaceID:       a.WorkspaceID,
		UserID:            a.UserID,
		IdempotencyKey:    a.IdempotencyKey,
		BusinessPhoneID:   a.BusinessPhoneID,
		TemplateID:        a.TemplateID,
		TemplateName:      a.TemplateName,
		Language:          a.Language,
		Category:          a.Category,
		ToNumber:          a.ToNumber,
		CampaignID:        a.CampaignID,
		EntryID:           a.EntryID,
		Status:            string(a.Status),
		ChargedMicros:     a.ChargedMicros,
		ProviderMessageID: a.ProviderMessageID,
		ResponseStatus:    a.ResponseStatus,
		ErrorCode:         a.ErrorCode,
		ErrorMessage:      a.ErrorMessage,
		ChargedAt:         a.ChargedAt,
		SentAt:            a.SentAt,
		RefundedAt:        a.RefundedAt,
	}
}

func toSendDomain(row *schema.WhatsAppTemplateSend) *template.SendAttempt {
	if row == nil {
		return nil
	}
	return &template.SendAttempt{
		ID:                row.ID,
		WorkspaceID:       row.WorkspaceID,
		UserID:            row.UserID,
		IdempotencyKey:    row.IdempotencyKey,
		BusinessPhoneID:   row.BusinessPhoneID,
		TemplateID:        row.TemplateID,
		TemplateName:      row.TemplateName,
		Language:          row.Language,
		Category:          row.Category,
		ToNumber:          row.ToNumber,
		CampaignID:        row.CampaignID,
		EntryID:           row.EntryID,
		Status:            template.SendAttemptStatus(row.Status),
		ChargedMicros:     row.ChargedMicros,
		ProviderMessageID: row.ProviderMessageID,
		ResponseStatus:    row.ResponseStatus,
		ErrorCode:         row.ErrorCode,
		ErrorMessage:      row.ErrorMessage,
		ChargedAt:         row.ChargedAt,
		SentAt:            row.SentAt,
		RefundedAt:        row.RefundedAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
