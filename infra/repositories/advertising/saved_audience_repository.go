package advertising_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type savedAudienceRepository struct {
	db *gorm.DB
}

func NewSavedAudienceRepository(db *gorm.DB) advertising.SavedAudienceRepository {
	return &savedAudienceRepository{db: db}
}

func (r *savedAudienceRepository) Create(ctx context.Context, s *advertising.SavedAudience) error {
	if blank(s.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	record, err := toSavedAudienceRecord(s)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	s.ID, s.CreatedAt, s.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *savedAudienceRepository) Update(ctx context.Context, s *advertising.SavedAudience) error {
	if blank(s.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	record, err := toSavedAudienceRecord(s)
	if err != nil {
		return err
	}
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&schema.AdSavedAudience{}).
		Where("id = ? AND workspace_id = ?", s.ID, s.WorkspaceID).
		Updates(map[string]any{
			"name":       record.Name,
			"targeting":  record.Targeting,
			"placements": record.Placements,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrSavedAudienceNotFound
	}
	s.UpdatedAt = now
	return nil
}

func (r *savedAudienceRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Delete(&schema.AdSavedAudience{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrSavedAudienceNotFound
	}
	return nil
}

func (r *savedAudienceRepository) Find(ctx context.Context, workspaceID, id string) (*advertising.SavedAudience, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdSavedAudience
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrSavedAudienceNotFound
		}
		return nil, err
	}
	return toSavedAudience(&record)
}

func (r *savedAudienceRepository) List(ctx context.Context, workspaceID string) ([]*advertising.SavedAudience, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdSavedAudience
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("name, id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.SavedAudience, 0, len(records))
	for i := range records {
		s, err := toSavedAudience(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func toSavedAudienceRecord(s *advertising.SavedAudience) (*schema.AdSavedAudience, error) {
	targeting, err := encodeJSON(s.Targeting)
	if err != nil {
		return nil, err
	}
	placements, err := encodeJSON(s.Placements)
	if err != nil {
		return nil, err
	}
	return &schema.AdSavedAudience{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Name:        s.Name,
		Targeting:   targeting,
		Placements:  placements,
		CreatedBy:   s.CreatedBy,
	}, nil
}

func toSavedAudience(record *schema.AdSavedAudience) (*advertising.SavedAudience, error) {
	s := &advertising.SavedAudience{
		ID:          record.ID,
		WorkspaceID: record.WorkspaceID,
		Name:        record.Name,
		CreatedBy:   record.CreatedBy,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
	if err := decodeJSON(record.Targeting, &s.Targeting); err != nil {
		return nil, err
	}
	if err := decodeJSON(record.Placements, &s.Placements); err != nil {
		return nil, err
	}
	return s, nil
}
