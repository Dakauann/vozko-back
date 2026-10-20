package workflowsecret_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/workflowsecret"
	"vozko/infra/database/schema"
)

const upsertSecretSQL = `INSERT INTO workflow_secrets (id, workspace_id, name, encrypted_value, created_by, updated_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, name) DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at
RETURNING id, created_by, created_at`

var errSecretNotSaved = errors.New("workflowsecret: the upsert returned no row")

type Repository struct {
	db *gorm.DB
}

var _ workflowsecret.Repository = (*Repository)(nil)

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Upsert(ctx context.Context, s workflowsecret.Secret) (workflowsecret.Secret, error) {
	if strings.TrimSpace(s.WorkspaceID) == "" {
		return workflowsecret.Secret{}, workflowsecret.ErrWorkspaceRequired
	}
	type kept struct {
		ID        string    `gorm:"column:id"`
		CreatedBy string    `gorm:"column:created_by"`
		CreatedAt time.Time `gorm:"column:created_at"`
	}
	var rows []kept
	if err := r.db.WithContext(ctx).Raw(upsertSecretSQL,
		s.ID, s.WorkspaceID, s.Name, s.EncryptedValue, s.CreatedBy, s.UpdatedBy, s.CreatedAt, s.UpdatedAt,
	).Scan(&rows).Error; err != nil {
		return workflowsecret.Secret{}, err
	}
	if len(rows) != 1 {
		return workflowsecret.Secret{}, errSecretNotSaved
	}
	s.ID = rows[0].ID
	s.CreatedBy = rows[0].CreatedBy
	s.CreatedAt = rows[0].CreatedAt
	return s, nil
}

func (r *Repository) Delete(ctx context.Context, workspaceID, name string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return workflowsecret.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).
		Where("workspace_id = ? AND name = ?", workspaceID, name).
		Delete(&schema.WorkflowSecret{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflowsecret.ErrNotFound
	}
	return nil
}

func (r *Repository) List(ctx context.Context, workspaceID string) ([]workflowsecret.Summary, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workflowsecret.ErrWorkspaceRequired
	}
	var rows []schema.WorkflowSecret
	if err := r.db.WithContext(ctx).
		Select("name", "updated_at", "updated_by").
		Where("workspace_id = ?", workspaceID).
		Order("name").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflowsecret.Summary, len(rows))
	for i, row := range rows {
		out[i] = workflowsecret.Summary{Name: row.Name, UpdatedAt: row.UpdatedAt, UpdatedBy: row.UpdatedBy}
	}
	return out, nil
}

func (r *Repository) FindByNames(ctx context.Context, workspaceID string, names []string) ([]workflowsecret.Secret, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workflowsecret.ErrWorkspaceRequired
	}
	if len(names) == 0 {
		return []workflowsecret.Secret{}, nil
	}
	var rows []schema.WorkflowSecret
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND name IN ?", workspaceID, names).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflowsecret.Secret, len(rows))
	for i, row := range rows {
		out[i] = workflowsecret.Secret{
			ID:             row.ID,
			WorkspaceID:    row.WorkspaceID,
			Name:           row.Name,
			EncryptedValue: row.EncryptedValue,
			CreatedBy:      row.CreatedBy,
			UpdatedBy:      row.UpdatedBy,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		}
	}
	return out, nil
}
