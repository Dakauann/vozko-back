package emailsending_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type domainRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDomainRepository(db *gorm.DB) es.DomainRepository {
	return &domainRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *domainRepository) take(q *gorm.DB) (*es.SendingDomain, error) {
	var row schema.EmailSendingDomain
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, es.ErrDomainNotFound
	}
	if err != nil {
		return nil, err
	}
	d, err := toDomain(row)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *domainRepository) Get(ctx context.Context, workspaceID, id string) (*es.SendingDomain, error) {
	return r.take(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id))
}

func (r *domainRepository) GetByDomain(ctx context.Context, domain string) (*es.SendingDomain, error) {
	return r.take(r.db.WithContext(ctx).Where("domain = ?", domain))
}

func (r *domainRepository) GetByReplyDomain(ctx context.Context, replyDomain string) (*es.SendingDomain, error) {
	return r.take(r.db.WithContext(ctx).Where("reply_domain = ?", replyDomain))
}

func (r *domainRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]es.SendingDomain, error) {
	var rows []schema.EmailSendingDomain
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomains(rows)
}

func (r *domainRepository) ListPending(ctx context.Context, limit int) ([]es.SendingDomain, error) {
	_, limit = page(0, limit)
	var rows []schema.EmailSendingDomain
	err := r.db.WithContext(ctx).
		Where("(status IN ? AND retired_at IS NULL) OR release_state = ?", []string{string(es.DomainPending), string(es.DomainVerifying)}, string(es.ReleasePending)).
		Order("last_checked_at NULLS FIRST, id").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomains(rows)
}

func (r *domainRepository) ListVerified(ctx context.Context, kind es.DomainKind, offset, limit int) ([]es.SendingDomain, error) {
	offset, limit = page(offset, limit)
	var rows []schema.EmailSendingDomain
	err := r.db.WithContext(ctx).
		Where("kind = ? AND status = ? AND retired_at IS NULL", string(kind), string(es.DomainVerified)).
		Order("created_at, id").Offset(offset).Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomains(rows)
}

func (r *domainRepository) Create(ctx context.Context, d *es.SendingDomain) error {
	now := r.now()
	d.ID = newID(d.ID)
	d.CreatedAt, d.UpdatedAt = now, now
	row, err := toDomainSchema(d)
	if err != nil {
		return err
	}
	if d.Retired {
		row.RetiredAt = &now
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return es.ErrDomainTaken
		}
		return err
	}
	return nil
}

func (r *domainRepository) Update(ctx context.Context, d *es.SendingDomain) error {
	now := r.now()
	d.UpdatedAt = now
	row, err := toDomainSchema(d)
	if err != nil {
		return err
	}
	var retired any
	if d.Retired {
		retired = gorm.Expr("COALESCE(retired_at, ?)", now)
	}
	retired = gorm.Expr("CASE WHEN release_state <> '' THEN retired_at ELSE ? END", retired)
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailSendingDomain{}).
		Where("workspace_id = ? AND id = ?", d.WorkspaceID, d.ID).
		Updates(map[string]any{
			"status":           row.Status,
			"connect_method":   row.ConnectMethod,
			"mail_from_domain": row.MailFromDomain,
			"reply_domain":     row.ReplyDomain,
			"records":          row.Records,
			"failure_reason":   row.FailureReason,
			"setup_failure":    row.SetupFailure,
			"verified_at":      row.VerifiedAt,
			"last_checked_at":  row.LastCheckedAt,
			"verifying_since":  row.VerifyingSince,
			"retired_at":       retired,
			"updated_at":       now,
		}), es.ErrDomainNotFound)
}

func (r *domainRepository) SetRelease(ctx context.Context, d *es.SendingDomain) error {
	now := r.now()
	var retired any
	if d.ReleaseState != es.ReleaseNone {
		retired = gorm.Expr("COALESCE(retired_at, ?)", now)
	}
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailSendingDomain{}).
		Where("workspace_id = ? AND id = ?", d.WorkspaceID, d.ID).
		Updates(map[string]any{"release_state": string(d.ReleaseState), "retired_at": retired, "updated_at": now}), es.ErrDomainNotFound)
}
