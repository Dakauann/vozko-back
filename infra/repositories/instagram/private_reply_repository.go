package instagram_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
)

type privateReplyRepository struct {
	db *gorm.DB
}

// NewPrivateReplyRepository builds the private-reply allowance guard.
func NewPrivateReplyRepository(db *gorm.DB) igdomain.PrivateReplyRepository {
	return &privateReplyRepository{db: db}
}

// Claim atomically reserves the single private reply permitted for a comment.
//
// Instagram allows exactly ONE private reply per comment, ever. A retry after an
// ambiguous failure (timeout, 5xx) would burn that allowance with no way to
// recover it, so the claim is an INSERT ... ON CONFLICT DO NOTHING against a
// primary key of the comment id and is written BEFORE the HTTP call. A zero
// RowsAffected means someone already holds the allowance and the caller must not
// issue the request.
func (r *privateReplyRepository) Claim(ctx context.Context, igCommentID, igAccountID string) (bool, error) {
	record := &schema.InstagramPrivateReply{
		IGCommentID: igCommentID,
		IGAccountID: igAccountID,
		Status:      string(igdomain.PrivateReplyAttempted),
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ig_comment_id"}},
			DoNothing: true,
		}).
		Create(record)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *privateReplyRepository) MarkSent(ctx context.Context, igCommentID, recipientIGSID, igMessageID string) error {
	updates := map[string]any{
		"status":     string(igdomain.PrivateReplySent),
		"updated_at": time.Now().UTC(),
	}
	if recipientIGSID != "" {
		updates["recipient_igsid"] = recipientIGSID
	}
	if igMessageID != "" {
		updates["ig_message_id"] = igMessageID
	}
	return r.update(ctx, igCommentID, updates)
}

// MarkFailed records a definitive failure.
//
// The row deliberately stays present: we cannot know whether Meta processed the
// send before failing to answer, so the allowance remains consumed rather than
// being handed back for a retry that could double-send.
func (r *privateReplyRepository) MarkFailed(ctx context.Context, igCommentID string, code int, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	return r.update(ctx, igCommentID, map[string]any{
		"status":        string(igdomain.PrivateReplyFailed),
		"error_code":    code,
		"error_message": message,
		"updated_at":    time.Now().UTC(),
	})
}

func (r *privateReplyRepository) Find(ctx context.Context, igCommentID string) (*igdomain.PrivateReply, error) {
	var record schema.InstagramPrivateReply
	if err := r.db.WithContext(ctx).
		First(&record, "ig_comment_id = ?", igCommentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &igdomain.PrivateReply{
		IGCommentID:    record.IGCommentID,
		IGAccountID:    record.IGAccountID,
		Status:         igdomain.PrivateReplyStatus(record.Status),
		RecipientIGSID: record.RecipientIGSID,
		IGMessageID:    record.IGMessageID,
		ErrorCode:      record.ErrorCode,
		ErrorMessage:   record.ErrorMessage,
		AttemptedAt:    record.AttemptedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}

func (r *privateReplyRepository) update(ctx context.Context, igCommentID string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramPrivateReply{}).
		Where("ig_comment_id = ?", igCommentID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrPrivateReplyUsed
	}
	return nil
}
