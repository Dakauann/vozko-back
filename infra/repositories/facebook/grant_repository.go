package facebook_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type grantRepository struct {
	db *gorm.DB
}

func NewGrantRepository(db *gorm.DB) fbdomain.GrantRepository {
	return &grantRepository{db: db}
}

func (r *grantRepository) Upsert(ctx context.Context, g *fbdomain.Grant) error {
	granular, err := json.Marshal(g.GranularScopes)
	if err != nil {
		return err
	}
	var existing schema.FacebookGrant
	err = r.db.WithContext(ctx).
		First(&existing, "workspace_id = ? AND app_scoped_user_id = ? AND client_business_id = ?",
			g.WorkspaceID, g.AppScopedUserID, g.ClientBusinessID).Error
	switch {
	case err == nil:
		g.ID = existing.ID
		return r.db.WithContext(ctx).Model(&schema.FacebookGrant{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"connected_by":     g.ConnectedBy,
			"token_kind":       string(g.TokenKind),
			"access_token":     piigorm.NewEncrypted(g.AccessToken),
			"token_expires_at": g.TokenExpiresAt,
			"scopes":           strings.Join(g.Scopes, ","),
			"granular_scopes":  datatypes.JSON(granular),
			"status":           string(fbdomain.GrantActive),
			"checked_at":       g.CheckedAt,
			"revoked_at":       nil,
		}).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		record := &schema.FacebookGrant{
			WorkspaceID:      g.WorkspaceID,
			ConnectedBy:      g.ConnectedBy,
			TokenKind:        string(g.TokenKind),
			AccessToken:      piigorm.NewEncrypted(g.AccessToken),
			TokenExpiresAt:   g.TokenExpiresAt,
			AppScopedUserID:  g.AppScopedUserID,
			ClientBusinessID: g.ClientBusinessID,
			Scopes:           strings.Join(g.Scopes, ","),
			GranularScopes:   datatypes.JSON(granular),
			Status:           string(fbdomain.GrantActive),
			CheckedAt:        g.CheckedAt,
		}
		if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
			return err
		}
		g.ID = record.ID
		return nil
	default:
		return err
	}
}

func (r *grantRepository) FindByID(ctx context.Context, id string) (*fbdomain.Grant, error) {
	var record schema.FacebookGrant
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrGrantNotFound
		}
		return nil, err
	}
	return toGrantDomain(&record)
}

func (r *grantRepository) ListActive(ctx context.Context, limit int) ([]*fbdomain.Grant, error) {
	var records []schema.FacebookGrant
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(fbdomain.GrantActive)).
		Order("checked_at ASC NULLS FIRST").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toGrants(records)
}

func (r *grantRepository) ListByAppScopedUser(ctx context.Context, appScopedUserID string) ([]*fbdomain.Grant, error) {
	var records []schema.FacebookGrant
	if err := r.db.WithContext(ctx).Find(&records, "app_scoped_user_id = ?", appScopedUserID).Error; err != nil {
		return nil, err
	}
	return toGrants(records)
}

func (r *grantRepository) MarkChecked(ctx context.Context, id string, scopes []string, granular map[string][]string, at time.Time) error {
	raw, err := json.Marshal(granular)
	if err != nil {
		return err
	}
	return r.update(ctx, id, map[string]any{
		"scopes":          strings.Join(scopes, ","),
		"granular_scopes": datatypes.JSON(raw),
		"checked_at":      at,
	})
}

func (r *grantRepository) Revoke(ctx context.Context, id string, at time.Time) error {
	return r.update(ctx, id, map[string]any{"status": string(fbdomain.GrantRevoked), "revoked_at": at})
}

func (r *grantRepository) EraseToken(ctx context.Context, id string) error {
	return r.update(ctx, id, map[string]any{"access_token": piigorm.NewEncrypted("")})
}

func (r *grantRepository) CountActivePages(ctx context.Context, grantID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&schema.FacebookPage{}).
		Where("grant_id = ? AND status = ?", grantID, string(fbdomain.StatusConnected)).
		Count(&count).Error
	return count, err
}

func (r *grantRepository) update(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.FacebookGrant{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrGrantNotFound
	}
	return nil
}

func toGrants(records []schema.FacebookGrant) ([]*fbdomain.Grant, error) {
	out := make([]*fbdomain.Grant, 0, len(records))
	for i := range records {
		g, err := toGrantDomain(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func toGrantDomain(record *schema.FacebookGrant) (*fbdomain.Grant, error) {
	granular := map[string][]string{}
	if len(record.GranularScopes) > 0 {
		if err := json.Unmarshal(record.GranularScopes, &granular); err != nil {
			return nil, err
		}
	}
	return &fbdomain.Grant{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		ConnectedBy:      record.ConnectedBy,
		TokenKind:        fbdomain.TokenKind(record.TokenKind),
		AccessToken:      record.AccessToken.Plain,
		TokenExpiresAt:   record.TokenExpiresAt,
		AppScopedUserID:  record.AppScopedUserID,
		ClientBusinessID: record.ClientBusinessID,
		Scopes:           splitList(record.Scopes),
		GranularScopes:   granular,
		Status:           fbdomain.GrantStatus(record.Status),
		CheckedAt:        record.CheckedAt,
		RevokedAt:        record.RevokedAt,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}, nil
}

func splitList(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
