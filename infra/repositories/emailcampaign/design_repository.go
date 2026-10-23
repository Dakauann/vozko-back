package emailcampaign

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domain "vozko/domain/emailcampaign"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type DesignRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDesignRepository(db *gorm.DB) *DesignRepository {
	return &DesignRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func designRow(d *domain.Design) (schema.EmailDesign, error) {
	doc, err := json.Marshal(d.Document)
	if err != nil {
		return schema.EmailDesign{}, err
	}
	return schema.EmailDesign{
		ID: d.ID, WorkspaceID: d.WorkspaceID, Name: d.Name, Document: doc, ThumbnailURL: d.ThumbnailURL,
		Kind: string(d.Kind), CreatedBy: schema.OptionalText(d.CreatedBy), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}, nil
}

func designOf(row schema.EmailDesign) (domain.Design, error) {
	d := domain.Design{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, ThumbnailURL: row.ThumbnailURL,
		Kind: domain.DesignKind(row.Kind), CreatedBy: string(row.CreatedBy), CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
	if !empty(row.Document) {
		if err := json.Unmarshal(row.Document, &d.Document); err != nil {
			return domain.Design{}, err
		}
	}
	return d, nil
}

func (r *DesignRepository) Create(ctx context.Context, d *domain.Design) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	now := r.now()
	d.CreatedAt, d.UpdatedAt = now, now
	row, err := designRow(d)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *DesignRepository) Get(ctx context.Context, workspaceID, id string) (*domain.Design, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, domain.ErrDesignNotFound
	}
	var row schema.EmailDesign
	if err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrDesignNotFound
		}
		return nil, err
	}
	d, err := designOf(row)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DesignRepository) Update(ctx context.Context, d *domain.Design) error {
	d.UpdatedAt = r.now()
	row, err := designRow(d)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.EmailDesign{}).Where("workspace_id = ? AND id = ?", d.WorkspaceID, d.ID).
		Updates(map[string]any{"name": row.Name, "document": row.Document, "thumbnail_url": row.ThumbnailURL, "kind": row.Kind, "updated_at": row.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrDesignNotFound
	}
	return nil
}

func (r *DesignRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if len(database.UUIDArray([]string{id})) == 0 {
		return domain.ErrDesignNotFound
	}
	result := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Delete(&schema.EmailDesign{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrDesignNotFound
	}
	return nil
}

func (r *DesignRepository) List(ctx context.Context, workspaceID string, kind domain.DesignKind, offset, limit int) ([]domain.Design, int64, error) {
	base := r.db.WithContext(ctx).Model(&schema.EmailDesign{}).Where("workspace_id = ?", workspaceID)
	if kind != "" {
		base = base.Where("kind = ?", string(kind))
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	var rows []schema.EmailDesign
	if err := base.Order("updated_at DESC, id").Offset(max(offset, 0)).Limit(min(limit, maxListLimit)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]domain.Design, 0, len(rows))
	for _, row := range rows {
		d, err := designOf(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, nil
}

var _ domain.DesignRepository = (*DesignRepository)(nil)
