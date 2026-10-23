package carriermessaging_repository

import (
	"context"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database/schema"
)

const phonesPerLookup = 1000

type suppressionRepository struct {
	db *gorm.DB
}

func NewSuppressionRepository(db *gorm.DB) carriermessaging.SuppressionRepository {
	return &suppressionRepository{db: db}
}

func (r *suppressionRepository) Add(ctx context.Context, s carriermessaging.Suppression) (bool, error) {
	if err := valid("suppression source", s.Source); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "workspace_id"}, {Name: "phone"}}, DoNothing: true}).
		Create(&schema.CarrierSuppression{
			ID:          newID(""),
			WorkspaceID: s.WorkspaceID,
			Phone:       s.Phone,
			Source:      string(s.Source),
			MessageID:   optionalID(s.MessageID),
			CreatedAt:   createdAt,
			UpdatedAt:   now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *suppressionRepository) Remove(ctx context.Context, workspaceID, phone string) error {
	return r.db.WithContext(ctx).Where("workspace_id = ? AND phone = ?", workspaceID, phone).Delete(&schema.CarrierSuppression{}).Error
}

func (r *suppressionRepository) IsSuppressed(ctx context.Context, workspaceID, phone string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&schema.CarrierSuppression{}).
		Where("workspace_id = ? AND phone = ?", workspaceID, phone).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *suppressionRepository) SuppressedAmong(ctx context.Context, workspaceID string, phones []string) (map[string]bool, error) {
	out := map[string]bool{}
	for chunk := range slices.Chunk(phones, phonesPerLookup) {
		var found []string
		if err := r.db.WithContext(ctx).Model(&schema.CarrierSuppression{}).
			Where("workspace_id = ? AND phone IN ?", workspaceID, chunk).
			Pluck("phone", &found).Error; err != nil {
			return nil, err
		}
		for _, phone := range found {
			out[phone] = true
		}
	}
	return out, nil
}

func (r *suppressionRepository) List(ctx context.Context, workspaceID string, limit, offset int) ([]carriermessaging.Suppression, int, error) {
	scope := r.db.WithContext(ctx).Model(&schema.CarrierSuppression{}).Where("workspace_id = ?", workspaceID)
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []schema.CarrierSuppression
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at DESC, phone ASC").
		Limit(limit).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	out := make([]carriermessaging.Suppression, 0, len(records))
	for _, record := range records {
		if err := valid("suppression source", carriermessaging.SuppressionSource(record.Source)); err != nil {
			return nil, 0, err
		}
		out = append(out, carriermessaging.Suppression{
			WorkspaceID: record.WorkspaceID,
			Phone:       record.Phone,
			Source:      carriermessaging.SuppressionSource(record.Source),
			MessageID:   idOrEmpty(record.MessageID),
			CreatedAt:   record.CreatedAt,
		})
	}
	return out, int(total), nil
}
