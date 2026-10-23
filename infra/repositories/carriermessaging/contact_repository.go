package carriermessaging_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) carriermessaging.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) take(query *gorm.DB) (*carriermessaging.Contact, error) {
	var record schema.CarrierContact
	if err := query.Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriermessaging.ErrContactNotFound
		}
		return nil, err
	}
	return toContact(&record), nil
}

func (r *contactRepository) findByPhone(ctx context.Context, workspaceID, phone string) (*carriermessaging.Contact, error) {
	return r.take(r.db.WithContext(ctx).Where("workspace_id = ? AND phone = ?", workspaceID, phone))
}

func (r *contactRepository) FindOrCreate(ctx context.Context, workspaceID, phone, displayName string) (*carriermessaging.Contact, error) {
	existing, err := r.findByPhone(ctx, workspaceID, phone)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, carriermessaging.ErrContactNotFound) {
		return nil, err
	}
	now := time.Now().UTC()
	record := &schema.CarrierContact{
		ID:          newID(""),
		WorkspaceID: workspaceID,
		Phone:       phone,
		DisplayName: strings.TrimSpace(displayName),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "phone"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoNothing:   true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.findByPhone(ctx, workspaceID, phone)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*carriermessaging.Contact, error) {
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*carriermessaging.Contact, error) {
	records, err := database.FindByIDs[schema.CarrierContact](ctx, r.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*carriermessaging.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContact(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) SetLead(ctx context.Context, id, leadID string) error {
	result := r.db.WithContext(ctx).Model(&schema.CarrierContact{}).Where("id = ?", id).Update("lead_id", optionalID(leadID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return carriermessaging.ErrContactNotFound
	}
	return nil
}

func (r *contactRepository) MarkFlooding(ctx context.Context, id string, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.CarrierContact{}).
		Where("id = ? AND flooding_at IS NULL", id).
		Update("flooding_at", at)
	return changedOnce(ctx, r.db, result, &schema.CarrierContact{}, id, carriermessaging.ErrContactNotFound)
}
