package notification_repository

import (
	"context"

	"gorm.io/gorm"

	"vozko/domain/notification"
	"vozko/domain/workspace"
)

const (
	membersSQL = "SELECT m.workspace_id::text AS workspace_id, u.id::text AS user_id, u.email, u.username AS name, m.role FROM workspace_members m" +
		" JOIN users u ON u.id = m.user_id JOIN workspaces w ON w.id = m.workspace_id" +
		" WHERE m.workspace_id = ANY(?::uuid[]) AND u.disabled_at IS NULL AND w.deleted_at IS NULL"
	workspaceNameSQL     = "SELECT name FROM workspaces WHERE id = ? AND deleted_at IS NULL"
	workspaceIDsSQL      = "SELECT id::text FROM workspaces WHERE deleted_at IS NULL ORDER BY id LIMIT ?"
	workspaceIDsAfterSQL = "SELECT id::text FROM workspaces WHERE deleted_at IS NULL AND id > ?::uuid ORDER BY id LIMIT ?"
)

type Directory struct {
	db *gorm.DB
}

var _ notification.Directory = (*Directory)(nil)

func NewDirectory(db *gorm.DB) *Directory {
	return &Directory{db: db}
}

type memberRow struct {
	WorkspaceID string
	UserID      string
	Email       string
	Name        string
	Role        string
}

func (d *Directory) Members(ctx context.Context, workspaceID string) ([]notification.Member, error) {
	byWorkspace, err := d.MembersOf(ctx, []string{workspaceID})
	if err != nil {
		return nil, err
	}
	return byWorkspace[workspaceID], nil
}

func (d *Directory) MembersOf(ctx context.Context, workspaceIDs []string) (map[string][]notification.Member, error) {
	out := map[string][]notification.Member{}
	ids := validUUIDs(workspaceIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []memberRow
	if err := d.db.WithContext(ctx).Raw(membersSQL, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkspaceID] = append(out[r.WorkspaceID], notification.Member{
			UserID: r.UserID, Email: r.Email, Name: r.Name, Manager: workspace.Role(r.Role).CanManageMembers(),
		})
	}
	return out, nil
}

func (d *Directory) WorkspaceName(ctx context.Context, workspaceID string) (string, error) {
	if !validUUID(workspaceID) {
		return "", notification.ErrNotFound
	}
	var names []string
	if err := d.db.WithContext(ctx).Raw(workspaceNameSQL, workspaceID).Scan(&names).Error; err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", notification.ErrNotFound
	}
	return names[0], nil
}

func (d *Directory) WorkspaceIDs(ctx context.Context, afterID string, limit int) ([]string, error) {
	var ids []string
	query := d.db.WithContext(ctx)
	var err error
	if afterID == "" {
		err = query.Raw(workspaceIDsSQL, limit).Scan(&ids).Error
	} else if validUUID(afterID) {
		err = query.Raw(workspaceIDsAfterSQL, afterID, limit).Scan(&ids).Error
	}
	return ids, err
}
