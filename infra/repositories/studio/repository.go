package studio_repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
)

const maxListLimit = 100

type repository struct {
	db *gorm.DB
}

var _ studio.Repository = (*repository)(nil)

func New(db *gorm.DB) studio.Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, p *studio.Project) error {
	record := &schema.StudioProject{
		WorkspaceID: p.WorkspaceID, Kind: string(p.Kind), Name: p.Name, Document: []byte(p.Document), Version: p.Version, CreatedBy: p.CreatedBy,
	}
	if p.Origin.Empty() {
		if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
			return err
		}
		p.ID, p.CreatedAt, p.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
		return nil
	}
	return r.createOnce(ctx, p, record)
}

func (r *repository) createOnce(ctx context.Context, p *studio.Project, record *schema.StudioProject) error {
	record.ChatThreadID, record.ScreenCommandID = &p.Origin.ThreadID, &p.Origin.CommandID
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "chat_thread_id"}, {Name: "screen_command_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: schema.StudioProjectOriginPredicate}}},
		DoNothing:   true,
	}).Create(record)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		p.ID, p.CreatedAt, p.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
		return nil
	}
	var existing schema.StudioProject
	err := r.db.WithContext(ctx).Where("chat_thread_id = ? AND screen_command_id = ?", p.Origin.ThreadID, p.Origin.CommandID).First(&existing).Error
	if err != nil {
		return err
	}
	if existing.WorkspaceID != p.WorkspaceID || existing.CreatedBy != p.CreatedBy {
		return studio.ErrOriginTaken
	}
	if existing.ArchivedAt != nil {
		return studio.ErrProjectNotFound
	}
	*p = *toDomain(&existing)
	return nil
}

func (r *repository) live(ctx context.Context, workspaceID string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.StudioProject{}).Where("workspace_id = ? AND archived_at IS NULL", workspaceID)
}

func (r *repository) Get(ctx context.Context, workspaceID, id string) (*studio.Project, error) {
	if !validID(id) || !validID(workspaceID) {
		return nil, studio.ErrProjectNotFound
	}
	var record schema.StudioProject
	err := r.live(ctx, workspaceID).Where("id = ?", id).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, studio.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(&record), nil
}

func (r *repository) List(ctx context.Context, q studio.ListQuery) ([]studio.Summary, int64, error) {
	if !validID(q.WorkspaceID) {
		return nil, 0, studio.ErrWorkspaceRequired
	}
	scope := r.live(ctx, q.WorkspaceID)
	if q.Kind != "" {
		scope = scope.Where("kind = ?", string(q.Kind))
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []schema.StudioProject
	err := scope.Select("id", "kind", "name", "version", "updated_at").Order("updated_at DESC").
		Limit(min(max(q.Limit, 1), maxListLimit)).Offset(max(q.Offset, 0)).Find(&records).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]studio.Summary, 0, len(records))
	for _, rec := range records {
		out = append(out, studio.Summary{ID: rec.ID, Kind: studio.Kind(rec.Kind), Name: rec.Name, Version: rec.Version, UpdatedAt: rec.UpdatedAt})
	}
	return out, total, nil
}

func (r *repository) Save(ctx context.Context, p *studio.Project, expectedVersion int64) error {
	if !validID(p.ID) || !validID(p.WorkspaceID) {
		return studio.ErrProjectNotFound
	}
	var saved []schema.StudioProject
	err := r.db.WithContext(ctx).Raw(`UPDATE studio_projects SET name = ?, document = ?, version = version + 1, updated_at = NOW()
		WHERE id = ? AND workspace_id = ? AND archived_at IS NULL AND version = ? RETURNING *`,
		p.Name, []byte(p.Document), p.ID, p.WorkspaceID, expectedVersion).Scan(&saved).Error
	if err != nil {
		return err
	}
	if len(saved) == 1 {
		p.Version, p.UpdatedAt = saved[0].Version, saved[0].UpdatedAt
		return nil
	}
	if _, err := r.Get(ctx, p.WorkspaceID, p.ID); err != nil {
		return err
	}
	return studio.ErrVersionConflict
}

func (r *repository) Archive(ctx context.Context, workspaceID, id string) error {
	if !validID(id) || !validID(workspaceID) {
		return studio.ErrProjectNotFound
	}
	result := r.live(ctx, workspaceID).Where("id = ?", id).Update("archived_at", time.Now().UTC())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return studio.ErrProjectNotFound
	}
	return nil
}

func toDomain(r *schema.StudioProject) *studio.Project {
	return &studio.Project{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Kind: studio.Kind(r.Kind), Name: r.Name, Document: []byte(r.Document),
		Version: r.Version, CreatedBy: r.CreatedBy, Origin: originOf(r), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func originOf(r *schema.StudioProject) studio.Origin {
	if r.ChatThreadID == nil || r.ScreenCommandID == nil {
		return studio.Origin{}
	}
	return studio.Origin{ThreadID: *r.ChatThreadID, CommandID: *r.ScreenCommandID}
}

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}
