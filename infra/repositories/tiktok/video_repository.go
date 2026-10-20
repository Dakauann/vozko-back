package tiktok_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

const upsertBatch = 200

type videoRepository struct {
	db *gorm.DB
}

func NewVideoRepository(db *gorm.DB) ttdomain.VideoRepository {
	return &videoRepository{db: db}
}

func (r *videoRepository) UpsertMany(ctx context.Context, videos []*ttdomain.Video) error {
	if len(videos) == 0 {
		return nil
	}
	records := make([]schema.TikTokVideo, 0, len(videos))
	now := time.Now().UTC()
	for _, v := range videos {
		if v == nil || !ttdomain.ValidNumericID(v.ItemID) || strings.TrimSpace(v.AccountID) == "" || strings.TrimSpace(v.WorkspaceID) == "" {
			continue
		}
		synced := v.SyncedAt
		if synced.IsZero() {
			synced = now
		}
		media := v.MediaType
		if media == "" {
			media = ttdomain.MediaTypeVideo
		}
		records = append(records, schema.TikTokVideo{
			WorkspaceID:  v.WorkspaceID,
			AccountID:    v.AccountID,
			ItemID:       v.ItemID,
			MediaType:    string(media),
			Caption:      v.Caption,
			ShareURL:     v.ShareURL,
			EmbedURL:     v.EmbedURL,
			ThumbnailKey: v.ThumbnailKey,
			IsAd:         v.IsAd,
			Likes:        v.Likes,
			Comments:     v.Comments,
			Shares:       v.Shares,
			Views:        v.Views,
			PostedAt:     v.PostedAt,
			SyncedAt:     synced,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}, {Name: "item_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"media_type":    gorm.Expr("EXCLUDED.media_type"),
			"caption":       gorm.Expr("EXCLUDED.caption"),
			"share_url":     gorm.Expr("EXCLUDED.share_url"),
			"embed_url":     gorm.Expr("EXCLUDED.embed_url"),
			"thumbnail_key": gorm.Expr("CASE WHEN EXCLUDED.thumbnail_key <> '' THEN EXCLUDED.thumbnail_key ELSE tiktok_videos.thumbnail_key END"),
			"is_ad":         gorm.Expr("EXCLUDED.is_ad"),
			"likes":         gorm.Expr("EXCLUDED.likes"),
			"comments":      gorm.Expr("EXCLUDED.comments"),
			"shares":        gorm.Expr("EXCLUDED.shares"),
			"views":         gorm.Expr("EXCLUDED.views"),
			"posted_at":     gorm.Expr("COALESCE(EXCLUDED.posted_at, tiktok_videos.posted_at)"),
			"synced_at":     gorm.Expr("EXCLUDED.synced_at"),
			"updated_at":    gorm.Expr("now()"),
		}),
	}).CreateInBatches(records, upsertBatch).Error
}

func (r *videoRepository) FindByItemID(ctx context.Context, accountID, itemID string) (*ttdomain.Video, error) {
	var record schema.TikTokVideo
	if err := r.db.WithContext(ctx).Where("account_id = ? AND item_id = ?", accountID, itemID).Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, ttdomain.ErrVideoNotFound
	}
	return toVideoDomain(&record), nil
}

func (r *videoRepository) FindByItemIDs(ctx context.Context, accountID string, itemIDs []string) ([]*ttdomain.Video, error) {
	unique := shared.UniqueNonEmpty(itemIDs)
	out := make([]*ttdomain.Video, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.TikTokVideo
		if err := r.db.WithContext(ctx).Where("account_id = ? AND item_id IN ?", accountID, unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toVideoDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *videoRepository) ListByAccount(ctx context.Context, in ttdomain.ListVideosInput) (*shared.PaginatedResult[*ttdomain.Video], error) {
	if strings.TrimSpace(in.AccountID) == "" {
		return nil, ttdomain.ErrAccountNotFound
	}
	query := r.db.WithContext(ctx).Model(&schema.TikTokVideo{}).Where("account_id = ?", in.AccountID)
	if s := strings.TrimSpace(in.Search); s != "" {
		query = query.Where("caption ILIKE ?", sqlexpr.LikeContains(s))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)
	var records []schema.TikTokVideo
	if err := query.Order("posted_at DESC NULLS LAST").Order("item_id DESC").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*ttdomain.Video, len(records))
	for i := range records {
		out[i] = toVideoDomain(&records[i])
	}
	return shared.NewPaginatedResult(out, pagination, total), nil
}

func (r *videoRepository) ListSlice(ctx context.Context, accountID string, limit, offset int) ([]*ttdomain.Video, error) {
	if limit <= 0 || limit > maxDueBatch {
		limit = maxDueBatch
	}
	if offset < 0 {
		offset = 0
	}
	var records []schema.TikTokVideo
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).
		Order("posted_at DESC NULLS LAST").Order("item_id DESC").
		Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*ttdomain.Video, len(records))
	for i := range records {
		out[i] = toVideoDomain(&records[i])
	}
	return out, nil
}
