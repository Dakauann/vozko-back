package carriermessaging_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/database/schema"
)

type senderRepository struct {
	db *gorm.DB
}

func NewSenderRepository(db *gorm.DB) carriermessaging.SenderRepository {
	return &senderRepository{db: db}
}

var awaitingProviderStatuses = []rcs.SenderStatus{rcs.SenderSubmitted, rcs.SenderAwaitingConfirmation}

func (r *senderRepository) Create(ctx context.Context, s *rcs.Sender) error {
	record, err := toSenderSchema(s)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	record.ID = newID(record.ID)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	s.ID, s.CreatedAt, s.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *senderRepository) Update(ctx context.Context, s *rcs.Sender) error {
	record, err := toSenderSchema(s)
	if err != nil {
		return err
	}
	record.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(record).
		Where("workspace_id = ?", s.WorkspaceID).
		Select("*").
		Omit("id", "workspace_id", "created_at", "deleted_at").
		Updates(record)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rcs.ErrSenderNotFound
	}
	s.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *senderRepository) first(ctx context.Context, query string, args ...any) (*rcs.Sender, error) {
	var record schema.RCSSender
	if err := r.db.WithContext(ctx).Where(query, args...).Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rcs.ErrSenderNotFound
		}
		return nil, err
	}
	return toSender(&record)
}

func (r *senderRepository) FindByID(ctx context.Context, workspaceID, id string) (*rcs.Sender, error) {
	return r.first(ctx, "id = ? AND workspace_id = ?", id, workspaceID)
}

func (r *senderRepository) FindByIDUnscoped(ctx context.Context, id string) (*rcs.Sender, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *senderRepository) FindByProfileID(ctx context.Context, provider, profileID string) (*rcs.Sender, error) {
	return r.first(ctx, "provider = ? AND provider_profile_id = ?", provider, profileID)
}

func (r *senderRepository) list(query *gorm.DB) ([]*rcs.Sender, error) {
	var records []schema.RCSSender
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}
	return toSenders(records)
}

func (r *senderRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*rcs.Sender, error) {
	return r.list(r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at DESC"))
}

func (r *senderRepository) ListAwaitingProvider(ctx context.Context, limit int) ([]*rcs.Sender, error) {
	return r.list(r.db.WithContext(ctx).Where("status IN ?", awaitingProviderStatuses).Order("updated_at ASC").Limit(limit))
}

func (r *senderRepository) ListAll(ctx context.Context, status *rcs.SenderStatus) ([]*rcs.Sender, error) {
	query := r.db.WithContext(ctx)
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	return r.list(query.Order("created_at DESC"))
}

func (r *senderRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.RCSSender{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rcs.ErrSenderNotFound
	}
	return nil
}
