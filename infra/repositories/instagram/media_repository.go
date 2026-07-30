package instagram_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
)

type mediaRepository struct {
	db *gorm.DB
}

// NewMediaRepository builds the Instagram media (posts) repository.
func NewMediaRepository(db *gorm.DB) igdomain.MediaRepository {
	return &mediaRepository{db: db}
}

// Upsert stores the durable projection of a post. Note that media_url and
// thumbnail_url are absent by design: they are short-lived signed CDN links, so
// they are fetched on demand and served through the proxy.
func (r *mediaRepository) Upsert(ctx context.Context, m *igdomain.Media) error {
	record := toMediaSchema(m)
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_media_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"caption", "media_type", "media_product_type", "permalink",
				"shortcode", "timestamp", "like_count", "comments_count",
				"is_comment_enabled", "updated_at",
			}),
		}).
		Create(record).Error
}

func (r *mediaRepository) UpsertMany(ctx context.Context, items []*igdomain.Media) error {
	if len(items) == 0 {
		return nil
	}
	records := make([]*schema.InstagramMedia, 0, len(items))
	for _, m := range items {
		if m != nil {
			records = append(records, toMediaSchema(m))
		}
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_media_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"caption", "media_type", "media_product_type", "permalink",
				"shortcode", "timestamp", "like_count", "comments_count",
				"is_comment_enabled", "updated_at",
			}),
		}).
		CreateInBatches(records, 100).Error
}

func (r *mediaRepository) FindByIGMediaID(ctx context.Context, igAccountID, igMediaID string) (*igdomain.Media, error) {
	var record schema.InstagramMedia
	if err := r.db.WithContext(ctx).
		First(&record, "ig_account_id = ? AND ig_media_id = ?", igAccountID, igMediaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrMediaNotFound
		}
		return nil, err
	}
	return toMediaDomain(&record), nil
}

func (r *mediaRepository) UpdateCounts(ctx context.Context, igAccountID, igMediaID string, likeCount, commentsCount int) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramMedia{}).
		Where("ig_account_id = ? AND ig_media_id = ?", igAccountID, igMediaID).
		Updates(map[string]any{"like_count": likeCount, "comments_count": commentsCount})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrMediaNotFound
	}
	return nil
}

func (r *mediaRepository) SetCommentEnabled(ctx context.Context, igAccountID, igMediaID string, enabled bool) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramMedia{}).
		Where("ig_account_id = ? AND ig_media_id = ?", igAccountID, igMediaID).
		Update("is_comment_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrMediaNotFound
	}
	return nil
}

func toMediaSchema(m *igdomain.Media) *schema.InstagramMedia {
	return &schema.InstagramMedia{
		ID:               m.ID,
		WorkspaceID:      m.WorkspaceID,
		IGAccountID:      m.IGAccountID,
		IGMediaID:        m.IGMediaID,
		MediaType:        string(m.MediaType),
		MediaProductType: string(m.MediaProductType),
		Caption:          m.Caption,
		Permalink:        m.Permalink,
		Shortcode:        m.Shortcode,
		Timestamp:        m.Timestamp,
		LikeCount:        m.LikeCount,
		CommentsCount:    m.CommentsCount,
		IsCommentEnabled: m.IsCommentEnabled,
	}
}

func toMediaDomain(record *schema.InstagramMedia) *igdomain.Media {
	return &igdomain.Media{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		IGAccountID:      record.IGAccountID,
		IGMediaID:        record.IGMediaID,
		MediaType:        igdomain.MediaType(record.MediaType),
		MediaProductType: igdomain.MediaProductType(record.MediaProductType),
		Caption:          record.Caption,
		Permalink:        record.Permalink,
		Shortcode:        record.Shortcode,
		Timestamp:        record.Timestamp,
		LikeCount:        record.LikeCount,
		CommentsCount:    record.CommentsCount,
		IsCommentEnabled: record.IsCommentEnabled,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}
