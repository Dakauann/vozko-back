package pipeline_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/pipeline"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

// NewRepository returns the domain pipeline.Repository backed by GORM.
func NewRepository(db *gorm.DB) pipeline.Repository {
	return &repository{db: db}
}

func (r *repository) Create(p *pipeline.Pipeline) error {
	row := schema.Pipeline{
		ID:           p.ID,
		WorkspaceID:  p.WorkspaceID,
		Name:         p.Name,
		ObjectType:   string(p.ObjectType),
		StageGroupID: p.StageGroupID,
		DepartmentID: p.DepartmentID,
		Position:     p.Position,
		IsDefault:    p.IsDefault,
	}
	if err := r.db.Create(&row).Error; err != nil {
		return err
	}
	p.ID = row.ID
	p.CreatedAt = row.CreatedAt
	p.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(p *pipeline.Pipeline) error {
	update := map[string]interface{}{
		"name":          p.Name,
		"object_type":   string(p.ObjectType),
		"department_id": nullableUUID(p.DepartmentID),
		"position":      p.Position,
		"is_default":    p.IsDefault,
	}
	res := r.db.Model(&schema.Pipeline{}).
		Where("id = ? AND workspace_id = ?", p.ID, p.WorkspaceID).
		Updates(update)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (r *repository) Delete(workspaceID, id string) error {
	res := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.Pipeline{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (r *repository) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	var row schema.Pipeline
	if err := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pipeline.ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&row), nil
}

func (r *repository) ListByWorkspace(workspaceID, objectType string) ([]*pipeline.Pipeline, error) {
	q := r.db.Where("workspace_id = ?", workspaceID)
	if strings.TrimSpace(objectType) != "" {
		q = q.Where("object_type = ?", objectType)
	}
	var rows []schema.Pipeline
	if err := q.Order("position ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*pipeline.Pipeline, len(rows))
	for i := range rows {
		out[i] = mapToDomain(&rows[i])
	}
	return out, nil
}

// PromoteDefault makes one funnel the workspace default for its object kind and
// demotes every other, in a single transaction.
//
// Demote-then-promote in that order, and both inside one transaction, because
// the partial unique index on (workspace_id, object_type) WHERE is_default
// would reject the promotion while the incumbent still holds the flag. The
// transaction is also what keeps a crash from leaving the workspace with zero
// defaults, which the stage repository "repairs" by minting another funnel.
func (r *repository) PromoteDefault(workspaceID, objectType, pipelineID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	objectType = strings.TrimSpace(objectType)
	pipelineID = strings.TrimSpace(pipelineID)
	if workspaceID == "" || objectType == "" || pipelineID == "" {
		return pipeline.ErrNotFound
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&schema.Pipeline{}).
			Where("workspace_id = ? AND object_type = ? AND id <> ? AND is_default = ?",
				workspaceID, objectType, pipelineID, true).
			Update("is_default", false).Error; err != nil {
			return err
		}

		res := tx.Model(&schema.Pipeline{}).
			Where("workspace_id = ? AND object_type = ? AND id = ?",
				workspaceID, objectType, pipelineID).
			Update("is_default", true)
		if res.Error != nil {
			return res.Error
		}
		// Nothing matched: the funnel is gone, belongs to another tenant, or
		// organizes a different object kind. Rolling back matters here — the
		// demotion above has already run, and committing it would leave the
		// workspace with no default at all.
		if res.RowsAffected == 0 {
			return pipeline.ErrNotFound
		}
		return nil
	})
}

// nullableUUID returns nil for an empty id so an Update writes SQL NULL instead
// of an empty string into a nullable uuid column (which Postgres would reject).
func nullableUUID(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func mapToDomain(row *schema.Pipeline) *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		Name:         row.Name,
		ObjectType:   pipeline.ObjectType(row.ObjectType),
		StageGroupID: row.StageGroupID,
		DepartmentID: row.DepartmentID,
		Position:     row.Position,
		IsDefault:    row.IsDefault,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
