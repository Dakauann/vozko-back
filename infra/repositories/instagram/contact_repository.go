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

type contactRepository struct {
	db *gorm.DB
}

// NewContactRepository builds the Instagram contact repository.
func NewContactRepository(db *gorm.DB) igdomain.ContactRepository {
	return &contactRepository{db: db}
}

// FindOrCreate resolves a contact by (account, IGSID).
//
// Identity is account-scoped on purpose: an Instagram-scoped ID is unique only
// within the (app, professional account) pair, so the same human legitimately has
// a different IGSID on each connected account and must become a separate contact.
//
// The insert is an upsert on the unique index so two consumers racing on the same
// first inbound message cannot create duplicates.
func (r *contactRepository) FindOrCreate(ctx context.Context, workspaceID, igAccountID, igsid string) (*igdomain.Contact, error) {
	existing, err := r.FindByIGSID(ctx, igAccountID, igsid)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, igdomain.ErrContactNotFound) {
		return nil, err
	}

	record := &schema.InstagramContact{
		WorkspaceID: workspaceID,
		IGAccountID: igAccountID,
		IGSID:       igsid,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_account_id"}, {Name: "igsid"}},
			// The unique index is PARTIAL (WHERE deleted_at IS NULL), because a soft-deleted
			// row must not block re-creating the same contact. Postgres will not infer a
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

	// OnConflict/DoNothing leaves RowsAffected at zero when another writer won
	// the race, so re-read rather than trusting the returned row.
	return r.FindByIGSID(ctx, igAccountID, igsid)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*igdomain.Contact, error) {
	var record schema.InstagramContact
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*igdomain.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.InstagramContact
	if err := r.db.WithContext(ctx).Find(&records, "id IN ?", ids).Error; err != nil {
		return nil, err
	}
	out := make([]*igdomain.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) FindByIGSID(ctx context.Context, igAccountID, igsid string) (*igdomain.Contact, error) {
	var record schema.InstagramContact
	if err := r.db.WithContext(ctx).
		First(&record, "ig_account_id = ? AND igsid = ?", igAccountID, igsid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p igdomain.ContactProfile) error {
	fetchedAt := p.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&schema.InstagramContact{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"username":                p.Username,
			"name":                    p.Name,
			"profile_picture_url":     p.ProfilePictureURL,
			"is_verified_user":        p.IsVerifiedUser,
			"follower_count":          p.FollowerCount,
			"is_user_follow_business": p.IsUserFollowBusiness,
			"is_business_follow_user": p.IsBusinessFollowUser,
			"profile_fetched_at":      fetchedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrContactNotFound
	}
	return nil
}

func (r *contactRepository) SetBlocked(ctx context.Context, id string, blocked bool) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramContact{}).
		Where("id = ?", id).Update("blocked", blocked)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrContactNotFound
	}
	return nil
}

func toContactDomain(record *schema.InstagramContact) *igdomain.Contact {
	return &igdomain.Contact{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		IGAccountID:          record.IGAccountID,
		IGSID:                record.IGSID,
		Username:             record.Username,
		Name:                 record.Name,
		ProfilePictureURL:    record.ProfilePictureURL,
		IsVerifiedUser:       record.IsVerifiedUser,
		FollowerCount:        record.FollowerCount,
		IsUserFollowBusiness: record.IsUserFollowBusiness,
		IsBusinessFollowUser: record.IsBusinessFollowUser,
		ProfileFetchedAt:     record.ProfileFetchedAt,
		LeadID:               record.LeadID,
		Blocked:              record.Blocked,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}
