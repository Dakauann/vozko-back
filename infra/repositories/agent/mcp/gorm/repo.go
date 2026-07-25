package gormmcp

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/database/schema"
)

type BuiltinBindingRepo struct{ db *gorm.DB }

func NewBuiltinBindingRepo(db *gorm.DB) *BuiltinBindingRepo { return &BuiltinBindingRepo{db: db} }

func (r *BuiltinBindingRepo) Upsert(ctx context.Context, b *domainmcp.BuiltinBinding) error {
	if b == nil || b.WorkspaceID == "" {
		return domainmcp.ErrWorkspaceRequired
	}
	row := bindingToRow(b)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"display_name", "label", "status", "auth_mode", "credential_ct", "kek_version",
			"expires_at", "refresh_hint", "metadata", "updated_at",
		}),
	}).Create(&row).Error
}

func (r *BuiltinBindingRepo) GetByID(ctx context.Context, ws, id string) (*domainmcp.BuiltinBinding, error) {
	var row schema.MCPBuiltinBinding
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", ws, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainmcp.ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToBinding(&row), nil
}

func (r *BuiltinBindingRepo) ListByWorkspace(ctx context.Context, ws string) ([]*domainmcp.BuiltinBinding, error) {
	var rows []schema.MCPBuiltinBinding
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", ws).Order("server_key ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domainmcp.BuiltinBinding, len(rows))
	for i := range rows {
		out[i] = rowToBinding(&rows[i])
	}
	return out, nil
}

func (r *BuiltinBindingRepo) Delete(ctx context.Context, ws, id string) error {
	res := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", ws, id).
		Delete(&schema.MCPBuiltinBinding{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainmcp.ErrBindingNotFound
	}
	return nil
}

type RemoteServerRepo struct{ db *gorm.DB }

func NewRemoteServerRepo(db *gorm.DB) *RemoteServerRepo { return &RemoteServerRepo{db: db} }

func (r *RemoteServerRepo) Create(ctx context.Context, s *domainmcp.RemoteMCPServer) error {
	if s == nil || s.WorkspaceID == "" {
		return domainmcp.ErrWorkspaceRequired
	}
	var existing int64
	if err := r.db.WithContext(ctx).Model(&schema.MCPRemoteServer{}).
		Where("workspace_id = ? AND url = ?", s.WorkspaceID, s.URL).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return domainmcp.ErrDuplicate
	}
	row := remoteToRow(s)
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *RemoteServerRepo) Update(ctx context.Context, s *domainmcp.RemoteMCPServer) error {
	if s == nil || s.WorkspaceID == "" {
		return domainmcp.ErrWorkspaceRequired
	}
	row := remoteToRow(s)
	res := r.db.WithContext(ctx).Model(&schema.MCPRemoteServer{}).
		Where("id = ? AND workspace_id = ?", s.ID, s.WorkspaceID).
		Updates(map[string]any{
			"name":                    row.Name,
			"url":                     row.URL,
			"transport":               row.Transport,
			"status":                  row.Status,
			"auth_mode":               row.AuthMode,
			"credential_ct":           row.CredentialCT,
			"kek_version":             row.KEKVersion,
			"expires_at":              row.ExpiresAt,
			"refresh_hint":            row.RefreshHint,
			"last_listed_at":          row.LastListedAt,
			"oauth_authz_url":         row.OAuthAuthzURL,
			"oauth_token_url":         row.OAuthTokenURL,
			"oauth_registration_url":  row.OAuthRegistrationURL,
			"oauth_scopes":            row.OAuthScopes,
			"oauth_resource":          row.OAuthResource,
			"oauth_client_id":         row.OAuthClientID,
			"oauth_client_secret_ct":  row.OAuthClientSecretCT,
			"oauth_client_secret_kek": row.OAuthClientSecretKEK,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainmcp.ErrRemoteServerNotFound
	}
	return nil
}

func (r *RemoteServerRepo) Get(ctx context.Context, ws, id string) (*domainmcp.RemoteMCPServer, error) {
	var row schema.MCPRemoteServer
	err := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, ws).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainmcp.ErrRemoteServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToRemote(&row), nil
}

func (r *RemoteServerRepo) ListByWorkspace(ctx context.Context, ws string) ([]*domainmcp.RemoteMCPServer, error) {
	var rows []schema.MCPRemoteServer
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", ws).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domainmcp.RemoteMCPServer, len(rows))
	for i := range rows {
		out[i] = rowToRemote(&rows[i])
	}
	return out, nil
}

func (r *RemoteServerRepo) Delete(ctx context.Context, ws, id string) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, ws).
		Delete(&schema.MCPRemoteServer{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainmcp.ErrRemoteServerNotFound
	}
	return nil
}

type ToolCacheRepo struct{ db *gorm.DB }

func NewToolCacheRepo(db *gorm.DB) *ToolCacheRepo { return &ToolCacheRepo{db: db} }

func (r *ToolCacheRepo) Replace(ctx context.Context, sourceID, ws string, tools []domainmcp.CachedTool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source_id = ? AND workspace_id = ?", sourceID, ws).
			Delete(&schema.MCPCachedTool{}).Error; err != nil {
			return err
		}
		if len(tools) == 0 {
			return nil
		}
		rows := make([]schema.MCPCachedTool, len(tools))
		for i, t := range tools {
			rows[i] = toolToRow(t)
		}
		return tx.Create(&rows).Error
	})
}

func (r *ToolCacheRepo) List(ctx context.Context, sourceID, ws string) ([]domainmcp.CachedTool, error) {
	var rows []schema.MCPCachedTool
	if err := r.db.WithContext(ctx).
		Where("source_id = ? AND workspace_id = ?", sourceID, ws).
		Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domainmcp.CachedTool, len(rows))
	for i := range rows {
		out[i] = rowToTool(&rows[i])
	}
	return out, nil
}

func (r *ToolCacheRepo) Purge(ctx context.Context, sourceID, ws string) error {
	return r.db.WithContext(ctx).
		Where("source_id = ? AND workspace_id = ?", sourceID, ws).
		Delete(&schema.MCPCachedTool{}).Error
}
