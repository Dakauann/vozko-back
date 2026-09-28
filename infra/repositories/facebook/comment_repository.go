package facebook_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/database/schema"
)

type commentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) fbdomain.CommentRepository {
	return &commentRepository{db: db}
}

func (r *commentRepository) UpsertMany(ctx context.Context, comments []*fbdomain.Comment) error {
	if len(comments) == 0 {
		return nil
	}
	records := make([]*schema.FacebookComment, 0, len(comments))
	for _, c := range comments {
		records = append(records, toCommentRecord(c))
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "fb_comment_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"from_name", "message", "like_count", "reply_count", "is_hidden", "liked_by_page", "updated_at",
			}),
		}).
		Create(&records).Error
}

func (r *commentRepository) FindByFBCommentID(ctx context.Context, fbCommentID string) (*fbdomain.Comment, error) {
	var record schema.FacebookComment
	if err := r.db.WithContext(ctx).First(&record, "fb_comment_id = ?", fbCommentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrCommentNotFound
		}
		return nil, err
	}
	return toCommentDomain(&record), nil
}

func (r *commentRepository) SetHidden(ctx context.Context, fbCommentID string, hidden bool) error {
	return r.set(ctx, fbCommentID, map[string]any{"is_hidden": hidden})
}

func (r *commentRepository) SetLiked(ctx context.Context, fbCommentID string, liked bool) error {
	return r.set(ctx, fbCommentID, map[string]any{"liked_by_page": liked})
}

func (r *commentRepository) Edit(ctx context.Context, fbCommentID, message string, at time.Time) error {
	return r.set(ctx, fbCommentID, map[string]any{"message": message, "edited_at": at})
}

func (r *commentRepository) MarkRemoved(ctx context.Context, fbCommentID string, at time.Time) error {
	return r.set(ctx, fbCommentID, map[string]any{"removed_at": at})
}

func (r *commentRepository) AddLikes(ctx context.Context, fbCommentID string, delta int) error {
	return r.set(ctx, fbCommentID, map[string]any{"like_count": gorm.Expr("GREATEST(like_count + ?, 0)", delta)})
}

func (r *commentRepository) LinkContact(ctx context.Context, fbCommentID, contactID string) error {
	return r.set(ctx, fbCommentID, map[string]any{"contact_id": contactID})
}

func (r *commentRepository) ContactsFor(ctx context.Context, fbCommentIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(fbCommentIDs))
	if len(fbCommentIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		FBCommentID string `gorm:"column:fb_comment_id"`
		ContactID   string `gorm:"column:contact_id"`
	}
	if err := r.db.WithContext(ctx).Model(&schema.FacebookComment{}).
		Select("fb_comment_id, contact_id").
		Where("fb_comment_id IN ? AND contact_id IS NOT NULL", fbCommentIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.FBCommentID] = row.ContactID
	}
	return out, nil
}

func (r *commentRepository) set(ctx context.Context, fbCommentID string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&schema.FacebookComment{}).Where("fb_comment_id = ?", fbCommentID).Updates(updates).Error
}

func toCommentRecord(c *fbdomain.Comment) *schema.FacebookComment {
	return &schema.FacebookComment{
		ID:                c.ID,
		WorkspaceID:       c.WorkspaceID,
		PageID:            c.PageID,
		FBCommentID:       c.FBCommentID,
		FBPostID:          c.FBPostID,
		ParentFBCommentID: c.ParentFBCommentID,
		FromID:            c.FromID,
		FromName:          c.FromName,
		FromIsPage:        c.FromIsPage,
		ContactID:         c.ContactID,
		Message:           c.Message,
		AttachmentType:    c.AttachmentType,
		LikeCount:         c.LikeCount,
		ReplyCount:        c.ReplyCount,
		IsHidden:          c.IsHidden,
		IsOurs:            c.IsOurs,
		LikedByPage:       c.LikedByPage,
		CreatedTime:       c.CreatedTime,
		EditedAt:          c.EditedAt,
		RemovedAt:         c.RemovedAt,
	}
}

func toCommentDomain(r *schema.FacebookComment) *fbdomain.Comment {
	return &fbdomain.Comment{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		PageID:            r.PageID,
		FBCommentID:       r.FBCommentID,
		FBPostID:          r.FBPostID,
		ParentFBCommentID: r.ParentFBCommentID,
		FromID:            r.FromID,
		FromName:          r.FromName,
		FromIsPage:        r.FromIsPage,
		ContactID:         r.ContactID,
		Message:           r.Message,
		AttachmentType:    r.AttachmentType,
		LikeCount:         r.LikeCount,
		ReplyCount:        r.ReplyCount,
		IsHidden:          r.IsHidden,
		IsOurs:            r.IsOurs,
		LikedByPage:       r.LikedByPage,
		CreatedTime:       r.CreatedTime,
		EditedAt:          r.EditedAt,
		RemovedAt:         r.RemovedAt,
	}
}
