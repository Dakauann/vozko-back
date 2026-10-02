package advertising_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/domain/facebook"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type grantRepository struct {
	db *gorm.DB
}

func NewGrantRepository(db *gorm.DB) advertising.GrantRepository {
	return &grantRepository{db: db}
}

func (r *grantRepository) Upsert(ctx context.Context, g *advertising.Grant) error {
	if blank(g.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	granular, err := granularJSON(g.GranularScopes)
	if err != nil {
		return err
	}
	var existing schema.AdGrant
	err = r.db.WithContext(ctx).
		First(&existing, "workspace_id = ? AND app_scoped_user_id = ? AND client_business_id = ?",
			g.WorkspaceID, g.AppScopedUserID, g.ClientBusinessID).Error
	switch {
	case err == nil:
		g.ID = existing.ID
		g.Status = advertising.GrantActive
		return r.db.WithContext(ctx).Model(&schema.AdGrant{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"connected_by":     g.ConnectedBy,
			"token_kind":       string(g.TokenKind),
			"access_token":     piigorm.NewEncrypted(g.AccessToken),
			"token_expires_at": g.TokenExpiresAt,
			"revoked_at":       nil,
			"scopes":           strings.Join(g.Scopes, ","),
			"status":           string(advertising.GrantActive),
			"granular_scopes":  granular,
			"checked_at":       g.CheckedAt,
		}).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		record := &schema.AdGrant{
			WorkspaceID:      g.WorkspaceID,
			ConnectedBy:      g.ConnectedBy,
			TokenKind:        string(g.TokenKind),
			AccessToken:      piigorm.NewEncrypted(g.AccessToken),
			TokenExpiresAt:   g.TokenExpiresAt,
			AppScopedUserID:  g.AppScopedUserID,
			ClientBusinessID: g.ClientBusinessID,
			Scopes:           strings.Join(g.Scopes, ","),
			GranularScopes:   granular,
			Status:           string(advertising.GrantActive),
			CheckedAt:        g.CheckedAt,
		}
		if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
			return err
		}
		g.ID, g.Status, g.CreatedAt, g.UpdatedAt = record.ID, advertising.GrantActive, record.CreatedAt, record.UpdatedAt
		return nil
	default:
		return err
	}
}

func (r *grantRepository) FindByID(ctx context.Context, id string) (*advertising.Grant, error) {
	var record schema.AdGrant
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrGrantNotFound
		}
		return nil, err
	}
	return toGrant(&record)
}

func (r *grantRepository) MarkChecked(ctx context.Context, id string, scopes []string, granular map[string][]string, at time.Time) error {
	raw, err := granularJSON(granular)
	if err != nil {
		return err
	}
	return r.update(ctx, id, map[string]any{
		"scopes":          strings.Join(scopes, ","),
		"granular_scopes": raw,
		"checked_at":      at,
	})
}

func (r *grantRepository) Revoke(ctx context.Context, id string, at time.Time) error {
	return r.update(ctx, id, map[string]any{"status": string(advertising.GrantRevoked), "revoked_at": at})
}

func (r *grantRepository) update(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.AdGrant{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrGrantNotFound
	}
	return nil
}

func granularJSON(granular map[string][]string) (datatypes.JSON, error) {
	if granular == nil {
		granular = map[string][]string{}
	}
	return encodeJSON(granular)
}

func toGrant(record *schema.AdGrant) (*advertising.Grant, error) {
	granular := map[string][]string{}
	if err := decodeJSON(record.GranularScopes, &granular); err != nil {
		return nil, err
	}
	return &advertising.Grant{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		ConnectedBy:      record.ConnectedBy,
		TokenKind:        facebook.TokenKind(record.TokenKind),
		AccessToken:      record.AccessToken.Plain,
		TokenExpiresAt:   record.TokenExpiresAt,
		AppScopedUserID:  record.AppScopedUserID,
		ClientBusinessID: record.ClientBusinessID,
		Scopes:           splitList(record.Scopes),
		GranularScopes:   granular,
		Status:           advertising.GrantStatus(record.Status),
		CheckedAt:        record.CheckedAt,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}, nil
}
