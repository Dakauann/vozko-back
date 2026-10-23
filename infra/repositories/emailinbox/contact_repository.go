package emailinbox_repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailaddress"
	"vozko/domain/emailinbox"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const maxDisplayNameRunes = 255

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) emailinbox.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) FindOrCreate(ctx context.Context, in emailinbox.FindOrCreateContactInput) (*emailinbox.Contact, error) {
	existing, err := r.FindByAddress(ctx, in.WorkspaceID, in.Address)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, emailinbox.ErrContactNotFound) {
		return nil, err
	}
	record := &schema.EmailContact{
		WorkspaceID: in.WorkspaceID,
		Address:     string(in.Address),
		DisplayName: boundedName(in.DisplayName),
		LeadID:      in.LeadID,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "address"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByAddress(ctx, in.WorkspaceID, in.Address)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*emailinbox.Contact, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *contactRepository) FindByAddress(ctx context.Context, workspaceID string, address emailaddress.Address) (*emailinbox.Contact, error) {
	return r.first(ctx, "workspace_id = ? AND address = ?", workspaceID, string(address))
}

func (r *contactRepository) first(ctx context.Context, query string, args ...any) (*emailinbox.Contact, error) {
	var record schema.EmailContact
	if err := r.db.WithContext(ctx).Where(query, args...).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, emailinbox.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*emailinbox.Contact, error) {
	records, err := database.FindByIDs[schema.EmailContact](ctx, r.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*emailinbox.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) UpdateDisplayName(ctx context.Context, id, displayName string) error {
	return r.affected(r.db.WithContext(ctx).Model(&schema.EmailContact{}).
		Where("id = ?", id).
		Update("display_name", boundedName(displayName)))
}

func (r *contactRepository) LinkLead(ctx context.Context, id, leadID string) error {
	return r.db.WithContext(ctx).Model(&schema.EmailContact{}).
		Where("id = ? AND lead_id IS NULL", id).
		Update("lead_id", leadID).Error
}

func (r *contactRepository) affected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return emailinbox.ErrContactNotFound
	}
	return nil
}

func boundedName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > maxDisplayNameRunes {
		return string(runes[:maxDisplayNameRunes])
	}
	return name
}

func toContactDomain(record *schema.EmailContact) *emailinbox.Contact {
	return &emailinbox.Contact{
		ID:          record.ID,
		WorkspaceID: record.WorkspaceID,
		Address:     emailaddress.Address(record.Address),
		DisplayName: record.DisplayName,
		LeadID:      record.LeadID,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
