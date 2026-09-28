package facebook_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) fbdomain.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) FindOrCreate(ctx context.Context, workspaceID, pageID, psid string) (*fbdomain.Contact, error) {
	existing, err := r.FindByPSID(ctx, pageID, psid)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, fbdomain.ErrContactNotFound) {
		return nil, err
	}
	record := &schema.FacebookContact{WorkspaceID: workspaceID, PageID: pageID, PSID: psid, ProfileStatus: string(fbdomain.ProfileUnknown)}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "page_id"}, {Name: "psid"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByPSID(ctx, pageID, psid)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*fbdomain.Contact, error) {
	var record schema.FacebookContact
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*fbdomain.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.FacebookContact
	if err := r.db.WithContext(ctx).Find(&records, "id IN ?", ids).Error; err != nil {
		return nil, err
	}
	out := make([]*fbdomain.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) FindByPSID(ctx context.Context, pageID, psid string) (*fbdomain.Contact, error) {
	var record schema.FacebookContact
	if err := r.db.WithContext(ctx).First(&record, "page_id = ? AND psid = ?", pageID, psid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p fbdomain.ContactProfile) error {
	updates := map[string]any{
		"profile_status":     string(p.Status),
		"profile_fetched_at": p.FetchedAt,
	}
	if p.Status == fbdomain.ProfileAvailable {
		updates["name"] = p.Name
		updates["first_name"] = p.FirstName
		updates["last_name"] = p.LastName
		if p.AvatarStorageKey != "" {
			updates["avatar_storage_key"] = p.AvatarStorageKey
		}
	}
	return r.update(ctx, id, updates)
}

func (r *contactRepository) MarkUnreachable(ctx context.Context, id, reason string) error {
	return r.update(ctx, id, map[string]any{"unreachable": true, "unreachable_reason": reason})
}

func (r *contactRepository) SetBlocked(ctx context.Context, id string, blocked bool) error {
	return r.update(ctx, id, map[string]any{"blocked": blocked})
}

func (r *contactRepository) update(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.FacebookContact{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrContactNotFound
	}
	return nil
}

func toContactDomain(record *schema.FacebookContact) *fbdomain.Contact {
	return &fbdomain.Contact{
		ID:                record.ID,
		WorkspaceID:       record.WorkspaceID,
		PageID:            record.PageID,
		PSID:              record.PSID,
		Name:              record.Name,
		FirstName:         record.FirstName,
		LastName:          record.LastName,
		AvatarStorageKey:  record.AvatarStorageKey,
		ProfileStatus:     fbdomain.ProfileStatus(record.ProfileStatus),
		ProfileFetchedAt:  record.ProfileFetchedAt,
		Unreachable:       record.Unreachable,
		UnreachableReason: record.UnreachableReason,
		LeadID:            record.LeadID,
		Blocked:           record.Blocked,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
}
