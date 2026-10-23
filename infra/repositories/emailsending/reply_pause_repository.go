package emailsending_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type replyPauseRepository struct {
	db *gorm.DB
}

func NewReplyPauseRepository(db *gorm.DB) es.ReplyPauses {
	return &replyPauseRepository{db: db}
}

func (r *replyPauseRepository) MarkRepliesPaused(ctx context.Context, workspaceID, domainID string, at time.Time) error {
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailSendingDomain{}).
		Where("workspace_id = ? AND id = ?", workspaceID, domainID).
		Update("replies_paused_at", gorm.Expr("COALESCE(replies_paused_at, ?)", at)), es.ErrDomainNotFound)
}

func (r *replyPauseRepository) ClearRepliesPaused(ctx context.Context, workspaceID, domainID string) error {
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailSendingDomain{}).
		Where("workspace_id = ? AND id = ?", workspaceID, domainID).
		Update("replies_paused_at", nil), es.ErrDomainNotFound)
}

const replyHoldMismatchesSQL = `
SELECT d.workspace_id, MIN(b.amount) AS balance_micros
FROM email_sending_domains d
JOIN balances b ON b.workspace_id = d.workspace_id
WHERE (d.replies_paused_at IS NOT NULL AND b.amount >= 0)
   OR (d.replies_paused_at IS NULL AND b.amount < 0 AND (d.retired_at IS NULL OR d.kind <> ?))
GROUP BY d.workspace_id
ORDER BY d.workspace_id`

func (r *replyPauseRepository) ListReplyHoldMismatches(ctx context.Context) ([]es.ReplyHold, error) {
	var rows []struct {
		WorkspaceID   string
		BalanceMicros int64
	}
	if err := r.db.WithContext(ctx).Raw(replyHoldMismatchesSQL, string(es.DomainOwn)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]es.ReplyHold, 0, len(rows))
	for _, row := range rows {
		out = append(out, es.ReplyHold{WorkspaceID: row.WorkspaceID, BalanceMicros: row.BalanceMicros})
	}
	return out, nil
}

func (r *replyPauseRepository) ListAll(ctx context.Context) ([]es.SendingDomain, error) {
	var rows []schema.EmailSendingDomain
	if err := r.db.WithContext(ctx).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomains(rows)
}
