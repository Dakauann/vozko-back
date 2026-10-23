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

const (
	maxPage         = 200
	slugUniqueIndex = "ux_email_sender_accounts_slug"
)

type accountRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAccountRepository(db *gorm.DB) es.AccountRepository {
	return &accountRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *accountRepository) first(ctx context.Context, column, value string) (*es.SenderAccount, error) {
	var row schema.EmailSenderAccount
	err := r.db.WithContext(ctx).Where(column+" = ?", value).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, es.ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	a := toAccount(row)
	return &a, nil
}

func (r *accountRepository) GetByWorkspace(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	return r.first(ctx, "workspace_id", workspaceID)
}

func (r *accountRepository) GetByTenantName(ctx context.Context, tenantName string) (*es.SenderAccount, error) {
	return r.first(ctx, "tenant_name", tenantName)
}

func (r *accountRepository) Create(ctx context.Context, a *es.SenderAccount) error {
	now := r.now()
	a.ID = newID(a.ID)
	a.CreatedAt, a.UpdatedAt = now, now
	return slugRefusal(r.db.WithContext(ctx).Create(toAccountSchema(a)).Error)
}

func (r *accountRepository) ChangeSlug(ctx context.Context, accountID, slug string) error {
	result := r.db.WithContext(ctx).Model(&schema.EmailSenderAccount{}).Where("id = ?", accountID).
		Updates(map[string]any{"slug": slug, "updated_at": r.now()})
	if err := slugRefusal(result.Error); err != nil {
		return err
	}
	return database.AffectedOne(result, es.ErrAccountNotFound)
}

func slugRefusal(err error) error {
	if database.IsUniqueViolationOf(err, slugUniqueIndex) {
		return es.ErrSlugTaken
	}
	return err
}

func (r *accountRepository) Update(ctx context.Context, a *es.SenderAccount) error {
	a.UpdatedAt = r.now()
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailSenderAccount{}).Where("id = ?", a.ID).Updates(map[string]any{
		"tenant_ref":           a.TenantRef,
		"mode":                 string(a.Mode),
		"status":               string(a.Status),
		"status_reason":        a.StatusReason,
		"probation_started_at": optionalTime(a.ProbationStartedAt),
		"promoted_at":          a.PromotedAt,
		"unused_warned_at":     a.UnusedWarnedAt,
		"updated_at":           a.UpdatedAt,
	}), es.ErrAccountNotFound)
}

func (r *accountRepository) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.EmailSenderAccount{}).Where("slug = ?", slug).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *accountRepository) ListByMode(ctx context.Context, mode es.Mode) ([]es.SenderAccount, error) {
	return r.listWhere(ctx, "mode = ?", string(mode))
}

func (r *accountRepository) ListByStatus(ctx context.Context, status es.AccountStatus) ([]es.SenderAccount, error) {
	return r.listWhere(ctx, "status = ?", string(status))
}

func (r *accountRepository) listWhere(ctx context.Context, condition, value string) ([]es.SenderAccount, error) {
	var rows []schema.EmailSenderAccount
	if err := r.db.WithContext(ctx).Where(condition, value).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]es.SenderAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccount(row))
	}
	return out, nil
}

func (r *accountRepository) List(ctx context.Context, offset, limit int) ([]es.SenderAccount, int64, error) {
	offset, limit = page(offset, limit)
	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.EmailSenderAccount{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []schema.EmailSenderAccount
	if err := r.db.WithContext(ctx).Order("created_at DESC, id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]es.SenderAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccount(row))
	}
	return out, total, nil
}

func page(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > maxPage {
		limit = maxPage
	}
	return offset, limit
}
