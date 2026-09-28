package metaplatform_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	mp "vozko/domain/metaplatform"
	"vozko/infra/database/schema"
)

type deletionRequestRepository struct {
	db *gorm.DB
}

func NewDeletionRequestRepository(db *gorm.DB) mp.DeletionRequestRepository {
	return &deletionRequestRepository{db: db}
}

func (r *deletionRequestRepository) Create(ctx context.Context, req *mp.DeletionRequest) error {
	return r.db.WithContext(ctx).Create(&schema.MetaDataDeletionRequest{
		Code:            req.Code,
		App:             string(req.App),
		AppScopedUserID: req.AppScopedUserID,
		Status:          string(req.Status),
		Detail:          req.Detail,
		RequestedAt:     req.RequestedAt,
		CompletedAt:     req.CompletedAt,
	}).Error
}

func (r *deletionRequestRepository) FindByCode(ctx context.Context, code string) (*mp.DeletionRequest, error) {
	var row schema.MetaDataDeletionRequest
	if err := r.db.WithContext(ctx).First(&row, "code = ?", code).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mp.ErrRequestNotFound
		}
		return nil, err
	}
	return &mp.DeletionRequest{
		Code:            row.Code,
		App:             mp.App(row.App),
		AppScopedUserID: row.AppScopedUserID,
		Status:          mp.DeletionStatus(row.Status),
		Detail:          row.Detail,
		RequestedAt:     row.RequestedAt,
		CompletedAt:     row.CompletedAt,
	}, nil
}

func (r *deletionRequestRepository) Finish(ctx context.Context, code string, status mp.DeletionStatus, detail string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.MetaDataDeletionRequest{}).
		Where("code = ?", code).
		Updates(map[string]any{"status": string(status), "detail": detail, "completed_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return mp.ErrRequestNotFound
	}
	return nil
}
