package tiktok_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const maxIDsPerQuery = 1000

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) ttdomain.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.TikTokContact{})
}

func (r *contactRepository) FindOrCreate(ctx context.Context, in ttdomain.FindOrCreateContactInput) (*ttdomain.Contact, error) {
	if !ttdomain.ValidOpaqueID(in.UniqueIdentifier) {
		return nil, ttdomain.ErrContactNotFound
	}
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.AccountID) == "" {
		return nil, ttdomain.ErrWorkspaceIDRequired
	}
	record := &schema.TikTokContact{
		WorkspaceID:      in.WorkspaceID,
		AccountID:        in.AccountID,
		UniqueIdentifier: in.UniqueIdentifier,
		Username:         strings.TrimSpace(in.Username),
		IsFollower:       in.IsFollower,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "account_id"}, {Name: "unique_identifier"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	contact, err := r.FindByUniqueIdentifier(ctx, in.AccountID, in.UniqueIdentifier)
	if err != nil {
		return nil, err
	}
	if username := strings.TrimSpace(in.Username); username != "" && contact.Username != username {
		if err := r.model(ctx).Where("id = ?", contact.ID).Update("username", username).Error; err != nil {
			return nil, err
		}
		contact.Username = username
	}
	return contact, nil
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*ttdomain.Contact, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *contactRepository) FindByUniqueIdentifier(ctx context.Context, accountID, uniqueIdentifier string) (*ttdomain.Contact, error) {
	return r.first(r.db.WithContext(ctx).Where("account_id = ? AND unique_identifier = ?", accountID, uniqueIdentifier))
}

func (r *contactRepository) first(query *gorm.DB) (*ttdomain.Contact, error) {
	var record schema.TikTokContact
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, ttdomain.ErrContactNotFound
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*ttdomain.Contact, error) {
	unique := shared.UniqueNonEmpty(ids)
	out := make([]*ttdomain.Contact, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.TikTokContact
		if err := r.db.WithContext(ctx).Where("id IN ?", unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toContactDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p ttdomain.ContactProfile, at time.Time) error {
	updates := map[string]any{"profile_refreshed_at": at}
	if u := strings.TrimSpace(p.Username); u != "" {
		updates["username"] = u
	}
	if d := strings.TrimSpace(p.DisplayName); d != "" {
		updates["display_name"] = d
	}
	if k := strings.TrimSpace(p.AvatarKey); k != "" {
		updates["avatar_key"] = k
	}
	if p.IsFollower != nil {
		updates["is_follower"] = *p.IsFollower
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(updates), ttdomain.ErrContactNotFound)
}

func (r *contactRepository) SetLeadID(ctx context.Context, id string, leadID *string) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("lead_id", leadID), ttdomain.ErrContactNotFound)
}
