package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

type serverRepository struct {
	db *gorm.DB
}

// NewServerRepository builds the provider-host repository.
func NewServerRepository(db *gorm.DB) uw.ServerRepository {
	return &serverRepository{db: db}
}

func (r *serverRepository) Create(ctx context.Context, s *uw.Server) error {
	record := toServerSchema(s)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	s.ID = record.ID
	s.CreatedAt = record.CreatedAt
	s.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *serverRepository) Update(ctx context.Context, s *uw.Server) error {
	record := toServerSchema(s)
	update := map[string]any{
		"workspace_id": record.WorkspaceID,
		"name":         record.Name,
		"provider":     record.Provider,
		"base_url":     record.BaseURL,
		"capacity":     record.Capacity,
		"enabled":      record.Enabled,
		"draining":     record.Draining,
	}
	// InUse is deliberately absent: it is owned by ClaimCapacity /
	// ReleaseCapacity / SyncCapacity, and letting a config save write it would
	// silently undo concurrent placements.
	if s.AdminToken != "" {
		update["admin_token"] = record.AdminToken
	}

	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppServer{}).
		Where("id = ?", s.ID).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrServerNotFound
	}
	return nil
}

func (r *serverRepository) FindByID(ctx context.Context, id string) (*uw.Server, error) {
	var record schema.UnofficialWhatsAppServer
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrServerNotFound
		}
		return nil, err
	}
	return toServerDomain(&record), nil
}

func (r *serverRepository) FindByBaseURL(ctx context.Context, baseURL string) (*uw.Server, error) {
	var record schema.UnofficialWhatsAppServer
	if err := r.db.WithContext(ctx).Where("base_url = ?", baseURL).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrServerNotFound
		}
		return nil, err
	}
	return toServerDomain(&record), nil
}

// ListPlacementCandidates returns the hosts a workspace may place on.
//
// Platform hosts (workspace_id IS NULL) plus that workspace's own, and never
// another workspace's: the admin token is host-wide, so placing a tenant on
// another tenant's host would hand them control of every instance on it.
func (r *serverRepository) ListPlacementCandidates(ctx context.Context, workspaceID string) ([]*uw.Server, error) {
	var records []schema.UnofficialWhatsAppServer
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND draining = ?", true, false).
		Where("workspace_id IS NULL OR workspace_id = ?", workspaceID).
		// Least loaded first, so placement spreads rather than filling one host
		// to its ceiling and then discovering the ceiling.
		Order("(in_use::float / NULLIF(capacity, 0)) ASC NULLS LAST").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return serversToDomain(records), nil
}

func (r *serverRepository) ListAll(ctx context.Context) ([]*uw.Server, error) {
	var records []schema.UnofficialWhatsAppServer
	if err := r.db.WithContext(ctx).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return serversToDomain(records), nil
}

// ClaimCapacity takes one slot if the host still has room.
//
// A conditional UPDATE rather than read-then-write: two concurrent connects on
// the last free slot would both pass a read check, and one tenant would be told
// the connection succeeded before the host refused it.
func (r *serverRepository) ClaimCapacity(ctx context.Context, serverID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppServer{}).
		Where("id = ? AND enabled = ? AND draining = ? AND capacity > 0 AND in_use < capacity",
			serverID, true, false).
		UpdateColumn("in_use", gorm.Expr("in_use + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ReleaseCapacity returns a slot. Floored at zero so a double release, which is
// the normal shape of a retried rollback, cannot drive the counter negative and
// make a full host look empty.
func (r *serverRepository) ReleaseCapacity(ctx context.Context, serverID string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppServer{}).
		Where("id = ? AND in_use > 0", serverID).
		UpdateColumn("in_use", gorm.Expr("in_use - 1")).Error
}

func (r *serverRepository) SyncCapacity(ctx context.Context, serverID string, inUse int) error {
	if inUse < 0 {
		inUse = 0
	}
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppServer{}).
		Where("id = ?", serverID).
		UpdateColumn("in_use", inUse).Error
}

func (r *serverRepository) RecordHealth(ctx context.Context, serverID string, healthyAt *time.Time, lastError string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppServer{}).
		Where("id = ?", serverID).
		Updates(map[string]any{
			"last_healthy_at": healthyAt,
			"last_error":      truncate(lastError, 500),
		}).Error
}

func serversToDomain(records []schema.UnofficialWhatsAppServer) []*uw.Server {
	out := make([]*uw.Server, 0, len(records))
	for i := range records {
		out = append(out, toServerDomain(&records[i]))
	}
	return out
}
