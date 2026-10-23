package apikey_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/apikey"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

var _ apikey.Repository = (*Repository)(nil)

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, k *apikey.Key) error {
	row, err := toRow(*k)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "api_keys:"+k.WorkspaceID).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&schema.APIKey{}).Where("workspace_id = ? AND revoked_at IS NULL", k.WorkspaceID).Count(&active).Error; err != nil {
			return err
		}
		if active >= apikey.MaxActivePerWorkspace {
			return apikey.ErrLimitReached
		}
		return tx.Create(&row).Error
	})
}

func (r *Repository) ListByWorkspace(ctx context.Context, workspaceID string) ([]apikey.Key, error) {
	var rows []schema.APIKey
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]apikey.Key, 0, len(rows))
	for _, row := range rows {
		k, err := fromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func (r *Repository) GetByLookupID(ctx context.Context, lookupID string) (*apikey.Key, error) {
	return r.first(r.db.WithContext(ctx).Where("lookup_id = ?", lookupID))
}

func (r *Repository) Get(ctx context.Context, workspaceID, id string) (*apikey.Key, error) {
	return r.first(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id))
}

func (r *Repository) first(q *gorm.DB) (*apikey.Key, error) {
	var row schema.APIKey
	if err := q.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apikey.ErrNotFound
		}
		return nil, err
	}
	k, err := fromRow(row)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *Repository) Revoke(ctx context.Context, workspaceID, id, by string, at time.Time) (*apikey.Key, error) {
	res := r.db.WithContext(ctx).Model(&schema.APIKey{}).
		Where("workspace_id = ? AND id = ? AND revoked_at IS NULL", workspaceID, id).
		Updates(map[string]any{"revoked_at": at, "revoked_by": by})
	if res.Error != nil {
		return nil, res.Error
	}
	return r.Get(ctx, workspaceID, id)
}

func (r *Repository) Touch(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.APIKey{}).Where("id = ?", id).Update("last_used_at", at).Error
}

func (r *Repository) CountActive(ctx context.Context, workspaceID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&schema.APIKey{}).Where("workspace_id = ? AND revoked_at IS NULL", workspaceID).Count(&n).Error
	return n, err
}

func toRow(k apikey.Key) (schema.APIKey, error) {
	scopes, err := json.Marshal(k.Scopes)
	if err != nil {
		return schema.APIKey{}, err
	}
	return schema.APIKey{
		ID: k.ID, WorkspaceID: k.WorkspaceID, Name: k.Name, LookupID: k.LookupID, SecretHash: k.SecretHash,
		Scopes: datatypes.JSON(scopes), CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt,
		RevokedAt: k.RevokedAt, RevokedBy: k.RevokedBy,
	}, nil
}

func fromRow(row schema.APIKey) (apikey.Key, error) {
	var scopes []apikey.Scope
	if err := json.Unmarshal(row.Scopes, &scopes); err != nil {
		return apikey.Key{}, fmt.Errorf("apikey: stored scopes of %s are unreadable: %w", row.ID, err)
	}
	return apikey.Key{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, LookupID: row.LookupID, SecretHash: row.SecretHash,
		Scopes: scopes, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt,
		RevokedAt: row.RevokedAt, RevokedBy: row.RevokedBy,
	}, nil
}
