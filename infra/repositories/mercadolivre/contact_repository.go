package mercadolivre_repository

import (
	"context"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) mldomain.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.MercadoLivreContact{})
}

func (r *contactRepository) FindOrCreateByUserID(ctx context.Context, in mldomain.FindOrCreateContactInput) (*mldomain.Contact, error) {
	if !mldomain.ValidNumericID(in.MLUserID) {
		return nil, mldomain.ErrUserIDInvalid
	}
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.AccountID) == "" {
		return nil, mldomain.ErrWorkspaceIDRequired
	}
	record := &schema.MercadoLivreContact{
		WorkspaceID: in.WorkspaceID,
		AccountID:   in.AccountID,
		MLUserID:    in.MLUserID,
		Nickname:    strings.TrimSpace(in.Nickname),
	}
	if record.Nickname != "" {
		now := time.Now().UTC()
		record.ProfileRefreshedAt = &now
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "account_id"}, {Name: "ml_user_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByUserID(ctx, in.AccountID, in.MLUserID)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*mldomain.Contact, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *contactRepository) FindByUserID(ctx context.Context, accountID, mlUserID string) (*mldomain.Contact, error) {
	return r.first(r.db.WithContext(ctx).Where("account_id = ? AND ml_user_id = ?", accountID, mlUserID))
}

func (r *contactRepository) first(query *gorm.DB) (*mldomain.Contact, error) {
	var record schema.MercadoLivreContact
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, mldomain.ErrContactNotFound
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*mldomain.Contact, error) {
	unique := shared.UniqueNonEmpty(ids)
	out := make([]*mldomain.Contact, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.MercadoLivreContact
		if err := r.db.WithContext(ctx).Where("id IN ?", unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toContactDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *contactRepository) UpdateNickname(ctx context.Context, id, nickname string, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"nickname":             strings.TrimSpace(nickname),
		"profile_refreshed_at": at,
	}), mldomain.ErrContactNotFound)
}

func (r *contactRepository) SetLeadID(ctx context.Context, id string, leadID *string) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("lead_id", leadID), mldomain.ErrContactNotFound)
}

func (r *contactRepository) SetQuestionsBlocked(ctx context.Context, id string, at *time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("questions_blocked_at", at), mldomain.ErrContactNotFound)
}

func (r *contactRepository) MarkQuestionsBlocked(ctx context.Context, accountID string, mlUserIDs []string, at, unchangedSince time.Time) error {
	unique := shared.UniqueNonEmpty(mlUserIDs)
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		if err := r.model(ctx).
			Where("account_id = ? AND ml_user_id IN ? AND questions_blocked_at IS NULL AND updated_at < ?", accountID, unique[start:end], unchangedSince).
			Update("questions_blocked_at", at).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *contactRepository) ClearQuestionsBlockedExcept(ctx context.Context, accountID string, keepMLUserIDs []string, blockedBefore time.Time) error {
	query := r.model(ctx).Where("account_id = ? AND questions_blocked_at < ?", accountID, blockedBefore)
	if keep := shared.UniqueNonEmpty(keepMLUserIDs); len(keep) > 0 {
		query = query.Where("NOT (ml_user_id = ANY(?))", pq.StringArray(keep))
	}
	return query.Update("questions_blocked_at", nil).Error
}

func toContactDomain(record *schema.MercadoLivreContact) *mldomain.Contact {
	return &mldomain.Contact{
		ID:                 record.ID,
		WorkspaceID:        record.WorkspaceID,
		AccountID:          record.AccountID,
		MLUserID:           record.MLUserID,
		Nickname:           record.Nickname,
		ProfileRefreshedAt: record.ProfileRefreshedAt,
		LeadID:             record.LeadID,
		QuestionsBlockedAt: record.QuestionsBlockedAt,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
	}
}
