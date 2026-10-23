package emailsending_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/infra/database"
)

const (
	workspaceNameSQL  = "SELECT name FROM workspaces WHERE id = ? AND deleted_at IS NULL"
	workspaceNamesSQL = "SELECT id::text AS id, name FROM workspaces WHERE id IN ? AND deleted_at IS NULL"
)

var ErrWorkspaceNotFound = errors.New("emailsending: workspace not found")

type WorkspaceNames struct {
	db *gorm.DB
}

func NewWorkspaceNames(db *gorm.DB) *WorkspaceNames {
	return &WorkspaceNames{db: db}
}

func (w *WorkspaceNames) WorkspaceName(ctx context.Context, workspaceID string) (string, error) {
	if len(database.UUIDArray([]string{workspaceID})) == 0 {
		return "", ErrWorkspaceNotFound
	}
	var names []string
	if err := w.db.WithContext(ctx).Raw(workspaceNameSQL, workspaceID).Scan(&names).Error; err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", ErrWorkspaceNotFound
	}
	return names[0], nil
}

func (w *WorkspaceNames) WorkspaceNamesByID(ctx context.Context, workspaceIDs []string) (map[string]string, error) {
	valid := database.UUIDArray(workspaceIDs)
	out := make(map[string]string, len(valid))
	if len(valid) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   string
		Name string
	}
	if err := w.db.WithContext(ctx).Raw(workspaceNamesSQL, []string(valid)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}
