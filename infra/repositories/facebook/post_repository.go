package facebook_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/database/schema"
)

type postRepository struct {
	db *gorm.DB
}

func NewPostRepository(db *gorm.DB) fbdomain.PostRepository {
	return &postRepository{db: db}
}

var keepOurAuthorship = clause.Assignment{
	Column: clause.Column{Name: "created_by_app"},
	Value:  gorm.Expr("facebook_posts.created_by_app OR excluded.created_by_app"),
}

func (r *postRepository) UpsertMany(ctx context.Context, posts []*fbdomain.Post) error {
	if len(posts) == 0 {
		return nil
	}
	records := make([]*schema.FacebookPost, 0, len(posts))
	for _, p := range posts {
		records = append(records, toPostRecord(p))
	}
	updates := clause.AssignmentColumns([]string{
		"kind", "status_type", "message", "permalink_url", "is_published", "scheduled_publish_time",
		"is_hidden", "reactions_count", "comments_count", "shares_count", "created_time", "updated_time", "updated_at",
	})
	updates = append(updates, keepOurAuthorship, clause.Assignment{Column: clause.Column{Name: "deleted_at"}, Value: nil})
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "fb_post_id"}}, DoUpdates: updates}).
		Create(&records).Error
}

func (r *postRepository) Track(ctx context.Context, post *fbdomain.Post) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "fb_post_id"}},
			DoUpdates: clause.Set{
				keepOurAuthorship,
				{Column: clause.Column{Name: "deleted_at"}, Value: nil},
			},
		}).
		Create(toPostRecord(post)).Error
}

func (r *postRepository) FindByFBPostID(ctx context.Context, fbPostID string) (*fbdomain.Post, error) {
	var record schema.FacebookPost
	if err := r.db.WithContext(ctx).First(&record, "fb_post_id = ?", fbPostID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrPostNotFound
		}
		return nil, err
	}
	return toPostDomain(&record), nil
}

func (r *postRepository) SetHidden(ctx context.Context, fbPostID string, hidden bool) error {
	return r.db.WithContext(ctx).Model(&schema.FacebookPost{}).
		Where("fb_post_id = ?", fbPostID).Update("is_hidden", hidden).Error
}

func (r *postRepository) UpdateMessage(ctx context.Context, fbPostID, message string) error {
	return r.db.WithContext(ctx).Model(&schema.FacebookPost{}).
		Where("fb_post_id = ?", fbPostID).Update("message", message).Error
}

func (r *postRepository) Remove(ctx context.Context, fbPostID string) error {
	return r.db.WithContext(ctx).Where("fb_post_id = ?", fbPostID).Delete(&schema.FacebookPost{}).Error
}

func toPostRecord(p *fbdomain.Post) *schema.FacebookPost {
	return &schema.FacebookPost{
		ID:                   p.ID,
		WorkspaceID:          p.WorkspaceID,
		PageID:               p.PageID,
		FBPostID:             p.FBPostID,
		Kind:                 string(p.Kind),
		StatusType:           p.StatusType,
		Message:              p.Message,
		PermalinkURL:         p.PermalinkURL,
		IsPublished:          p.IsPublished,
		ScheduledPublishTime: p.ScheduledPublishTime,
		IsHidden:             p.IsHidden,
		CreatedByApp:         p.CreatedByApp,
		ReactionsCount:       p.ReactionsCount,
		CommentsCount:        p.CommentsCount,
		SharesCount:          p.SharesCount,
		CreatedTime:          p.CreatedTime,
		UpdatedTime:          p.UpdatedTime,
	}
}

func toPostDomain(r *schema.FacebookPost) *fbdomain.Post {
	return &fbdomain.Post{
		ID:                   r.ID,
		WorkspaceID:          r.WorkspaceID,
		PageID:               r.PageID,
		FBPostID:             r.FBPostID,
		Kind:                 fbdomain.PostKind(r.Kind),
		StatusType:           r.StatusType,
		Message:              r.Message,
		PermalinkURL:         r.PermalinkURL,
		IsPublished:          r.IsPublished,
		ScheduledPublishTime: r.ScheduledPublishTime,
		IsHidden:             r.IsHidden,
		CreatedByApp:         r.CreatedByApp,
		ReactionsCount:       r.ReactionsCount,
		CommentsCount:        r.CommentsCount,
		SharesCount:          r.SharesCount,
		CreatedTime:          r.CreatedTime,
		UpdatedTime:          r.UpdatedTime,
	}
}

func (r *postRepository) AppMadeAmong(ctx context.Context, fbPostIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(fbPostIDs))
	if len(fbPostIDs) == 0 {
		return out, nil
	}
	var ids []string
	if err := r.db.WithContext(ctx).Model(&schema.FacebookPost{}).
		Where("fb_post_id IN ? AND created_by_app", fbPostIDs).
		Pluck("fb_post_id", &ids).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func (r *postRepository) AddCounts(ctx context.Context, fbPostID string, reactions, comments int) error {
	return r.db.WithContext(ctx).Model(&schema.FacebookPost{}).Where("fb_post_id = ?", fbPostID).Updates(map[string]any{
		"reactions_count": gorm.Expr("GREATEST(reactions_count + ?, 0)", reactions),
		"comments_count":  gorm.Expr("GREATEST(comments_count + ?, 0)", comments),
	}).Error
}

func (r *postRepository) ListByPage(ctx context.Context, pageID string, limit, offset int) ([]*fbdomain.Post, error) {
	var records []schema.FacebookPost
	if err := r.db.WithContext(ctx).Where("page_id = ?", pageID).
		Order("created_time DESC NULLS LAST").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*fbdomain.Post, 0, len(records))
	for i := range records {
		out = append(out, toPostDomain(&records[i]))
	}
	return out, nil
}
