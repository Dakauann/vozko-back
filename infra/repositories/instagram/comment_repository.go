package instagram_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type commentRepository struct {
	db *gorm.DB
}

// NewCommentRepository builds the Instagram comment repository.
func NewCommentRepository(db *gorm.DB) igdomain.CommentRepository {
	return &commentRepository{db: db}
}

func (r *commentRepository) Upsert(ctx context.Context, c *igdomain.Comment) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_comment_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"text", "like_count", "hidden", "parent_ig_comment_id",
				"from_igsid", "from_username", "is_ours", "timestamp", "updated_at",
			}),
		}).
		Create(toCommentSchema(c)).Error
}

func (r *commentRepository) UpsertMany(ctx context.Context, items []*igdomain.Comment) error {
	if len(items) == 0 {
		return nil
	}
	records := make([]*schema.InstagramComment, 0, len(items))
	for _, c := range items {
		if c != nil {
			records = append(records, toCommentSchema(c))
		}
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ig_comment_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"text", "like_count", "hidden", "parent_ig_comment_id",
				"from_igsid", "from_username", "is_ours", "timestamp", "updated_at",
			}),
		}).
		CreateInBatches(records, 100).Error
}

func (r *commentRepository) FindByIGCommentID(ctx context.Context, igAccountID, igCommentID string) (*igdomain.Comment, error) {
	var record schema.InstagramComment
	if err := r.db.WithContext(ctx).
		First(&record, "ig_account_id = ? AND ig_comment_id = ?", igAccountID, igCommentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrCommentNotFound
		}
		return nil, err
	}
	return toCommentDomain(&record), nil
}

func (r *commentRepository) SetHidden(ctx context.Context, igAccountID, igCommentID string, hidden bool) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramComment{}).
		Where("ig_account_id = ? AND ig_comment_id = ?", igAccountID, igCommentID).
		Update("hidden", hidden)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrCommentNotFound
	}
	return nil
}

func (r *commentRepository) Delete(ctx context.Context, igAccountID, igCommentID string) error {
	result := r.db.WithContext(ctx).
		Where("ig_account_id = ? AND ig_comment_id = ?", igAccountID, igCommentID).
		Delete(&schema.InstagramComment{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrCommentNotFound
	}
	return nil
}

// ListByMedia returns the locally mirrored comments for a post.
//
// Ordering is newest-first to match Graph's reverse-chronological edge, so the UI
// shows the same order whether a page came from the mirror or from a live fetch.
func (r *commentRepository) ListByMedia(ctx context.Context, input igdomain.ListCommentsInput) (*shared.PaginatedResult[*igdomain.Comment], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	query := r.db.WithContext(ctx).Model(&schema.InstagramComment{}).
		Where("ig_account_id = ? AND ig_media_id = ?", input.IGAccountID, input.IGMediaID)

	if input.TopLevelOnly {
		query = query.Where("parent_ig_comment_id IS NULL")
	}
	if input.HiddenOnly != nil {
		query = query.Where("hidden = ?", *input.HiddenOnly)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var records []schema.InstagramComment
	if err := query.
		Order("timestamp DESC NULLS LAST").
		Limit(pagination.PageSize).
		Offset(pagination.Offset()).
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*igdomain.Comment, 0, len(records))
	for i := range records {
		items = append(items, toCommentDomain(&records[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func toCommentSchema(c *igdomain.Comment) *schema.InstagramComment {
	return &schema.InstagramComment{
		ID:                c.ID,
		WorkspaceID:       c.WorkspaceID,
		IGAccountID:       c.IGAccountID,
		IGCommentID:       c.IGCommentID,
		IGMediaID:         c.IGMediaID,
		ParentIGCommentID: c.ParentIGCommentID,
		FromIGSID:         c.FromIGSID,
		FromUsername:      c.FromUsername,
		Text:              c.Text,
		LikeCount:         c.LikeCount,
		Hidden:            c.Hidden,
		IsOurs:            c.IsOurs,
		Timestamp:         c.Timestamp,
	}
}

func toCommentDomain(record *schema.InstagramComment) *igdomain.Comment {
	return &igdomain.Comment{
		ID:                record.ID,
		WorkspaceID:       record.WorkspaceID,
		IGAccountID:       record.IGAccountID,
		IGCommentID:       record.IGCommentID,
		IGMediaID:         record.IGMediaID,
		ParentIGCommentID: record.ParentIGCommentID,
		FromIGSID:         record.FromIGSID,
		FromUsername:      record.FromUsername,
		Text:              record.Text,
		LikeCount:         record.LikeCount,
		Hidden:            record.Hidden,
		IsOurs:            record.IsOurs,
		Timestamp:         record.Timestamp,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
}
