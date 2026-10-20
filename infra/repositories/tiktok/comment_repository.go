package tiktok_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const applyCommentEventSQL = `INSERT INTO tiktok_comments
	(id, workspace_id, account_id, video_id, comment_id, parent_comment_id, kind, contact_id, author_unique_identifier,
	 text, visibility, is_ours, commented_at, last_event_at, lookup, lookup_attempts, next_lookup_at, rules_due, created_at, updated_at)
VALUES (gen_random_uuid(), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, now(), now())
ON CONFLICT (account_id, comment_id) DO UPDATE SET
	visibility = EXCLUDED.visibility,
	text = CASE WHEN EXCLUDED.text <> '' THEN EXCLUDED.text ELSE tiktok_comments.text END,
	author_unique_identifier = CASE WHEN tiktok_comments.author_unique_identifier = '' THEN EXCLUDED.author_unique_identifier ELSE tiktok_comments.author_unique_identifier END,
	contact_id = COALESCE(tiktok_comments.contact_id, EXCLUDED.contact_id),
	commented_at = COALESCE(tiktok_comments.commented_at, EXCLUDED.commented_at),
	last_event_at = EXCLUDED.last_event_at,
	rules_due = CASE
		WHEN EXCLUDED.visibility = 'deleted' THEN false
		WHEN EXCLUDED.rules_due AND tiktok_comments.is_ours IS DISTINCT FROM true THEN true
		ELSE tiktok_comments.rules_due END,
	next_lookup_at = CASE WHEN EXCLUDED.rules_due THEN COALESCE(tiktok_comments.next_lookup_at, EXCLUDED.next_lookup_at) ELSE tiktok_comments.next_lookup_at END,
	updated_at = now()
WHERE tiktok_comments.last_event_at IS NULL OR tiktok_comments.last_event_at < EXCLUDED.last_event_at`

const claimCommentWorkSQL = `WITH due AS (
	SELECT id FROM tiktok_comments
	WHERE (lookup = 'pending' OR rules_due) AND (next_lookup_at IS NULL OR next_lookup_at <= ?)
	ORDER BY next_lookup_at NULLS FIRST, id
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
UPDATE tiktok_comments t SET next_lookup_at = ?, updated_at = now()
FROM due WHERE t.id = due.id
RETURNING t.*`

type commentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) ttdomain.CommentRepository {
	return &commentRepository{db: db}
}

func (r *commentRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.TikTokComment{})
}

func (r *commentRepository) ApplyEvent(ctx context.Context, w ttdomain.CommentEventWrite) (bool, error) {
	ev := w.Event
	if !ttdomain.ValidNumericID(ev.CommentID) || !ttdomain.ValidNumericID(ev.VideoID) || strings.TrimSpace(w.AccountID) == "" || strings.TrimSpace(w.WorkspaceID) == "" {
		return false, ttdomain.ErrInvalidContent
	}
	at := w.OccurredAt.UTC()
	inserted := ev.Action == ttdomain.CommentInserted
	lookup := ttdomain.LookupResolved
	rulesDue := false
	var isOurs *bool
	if inserted {
		if w.KnownOurs {
			ours := true
			isOurs = &ours
		} else {
			lookup = ttdomain.LookupPending
			rulesDue = true
		}
	}
	var commentedAt *time.Time
	if inserted {
		commentedAt = &at
	}
	result := r.db.WithContext(ctx).Exec(applyCommentEventSQL,
		w.WorkspaceID, w.AccountID, ev.VideoID, ev.CommentID, ev.ParentCommentID, string(ev.Kind), w.ContactID, ev.PersonID,
		ev.Text, string(ev.Action.Visibility()), isOurs, commentedAt, at, string(lookup), at, rulesDue,
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *commentRepository) RecordOurs(ctx context.Context, c *ttdomain.Comment) error {
	if c == nil || !ttdomain.ValidNumericID(c.CommentID) || !ttdomain.ValidNumericID(c.VideoID) {
		return ttdomain.ErrInvalidContent
	}
	ours := true
	kind := c.Kind
	if kind == "" {
		kind = ttdomain.KindReply
	}
	now := time.Now().UTC()
	commentedAt := c.CommentedAt
	if commentedAt == nil {
		commentedAt = &now
	}
	record := schema.TikTokComment{
		WorkspaceID:       c.WorkspaceID,
		AccountID:         c.AccountID,
		VideoID:           c.VideoID,
		CommentID:         c.CommentID,
		ParentCommentID:   c.ParentCommentID,
		Kind:              string(kind),
		AuthorUsername:    c.AuthorUsername,
		AuthorDisplayName: c.AuthorDisplayName,
		Text:              c.Text,
		Visibility:        string(ttdomain.VisibilityPublic),
		IsOurs:            &ours,
		CommentedAt:       commentedAt,
		Lookup:            string(ttdomain.LookupResolved),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}, {Name: "comment_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"is_ours":    true,
			"rules_due":  false,
			"lookup":     string(ttdomain.LookupResolved),
			"text":       gorm.Expr("CASE WHEN tiktok_comments.text = '' THEN EXCLUDED.text ELSE tiktok_comments.text END"),
			"updated_at": gorm.Expr("now()"),
		}),
	}).Create(&record).Error
}

func (r *commentRepository) MirrorMany(ctx context.Context, comments []*ttdomain.Comment) error {
	records := make([]schema.TikTokComment, 0, len(comments))
	for _, c := range comments {
		if c == nil || !ttdomain.ValidNumericID(c.CommentID) || !ttdomain.ValidNumericID(c.VideoID) || c.AccountID == "" || c.WorkspaceID == "" {
			continue
		}
		kind := c.Kind
		if kind == "" {
			kind = ttdomain.KindComment
		}
		records = append(records, schema.TikTokComment{
			WorkspaceID:            c.WorkspaceID,
			AccountID:              c.AccountID,
			VideoID:                c.VideoID,
			CommentID:              c.CommentID,
			ParentCommentID:        c.ParentCommentID,
			Kind:                   string(kind),
			ContactID:              c.ContactID,
			AuthorUniqueIdentifier: c.AuthorUniqueIdentifier,
			AuthorUsername:         c.AuthorUsername,
			AuthorDisplayName:      c.AuthorDisplayName,
			Text:                   c.Text,
			ImageURL:               c.ImageURL,
			Visibility:             string(c.Visibility),
			IsOurs:                 c.IsOurs,
			LikedByUs:              c.LikedByUs,
			Likes:                  c.Likes,
			Replies:                c.Replies,
			CommentedAt:            c.CommentedAt,
			Lookup:                 string(ttdomain.LookupResolved),
		})
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}, {Name: "comment_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"contact_id":               gorm.Expr("COALESCE(EXCLUDED.contact_id, tiktok_comments.contact_id)"),
			"author_unique_identifier": gorm.Expr("CASE WHEN EXCLUDED.author_unique_identifier <> '' THEN EXCLUDED.author_unique_identifier ELSE tiktok_comments.author_unique_identifier END"),
			"author_username":          gorm.Expr("EXCLUDED.author_username"),
			"author_display_name":      gorm.Expr("EXCLUDED.author_display_name"),
			"text":                     gorm.Expr("EXCLUDED.text"),
			"image_url":                gorm.Expr("EXCLUDED.image_url"),
			"visibility":               gorm.Expr("CASE WHEN tiktok_comments.visibility = 'deleted' OR EXCLUDED.visibility = '' THEN tiktok_comments.visibility ELSE EXCLUDED.visibility END"),
			"is_ours":                  gorm.Expr("EXCLUDED.is_ours"),
			"liked_by_us":              gorm.Expr("EXCLUDED.liked_by_us"),
			"likes":                    gorm.Expr("EXCLUDED.likes"),
			"replies":                  gorm.Expr("EXCLUDED.replies"),
			"commented_at":             gorm.Expr("COALESCE(EXCLUDED.commented_at, tiktok_comments.commented_at)"),
			"lookup":                   string(ttdomain.LookupResolved),
			"rules_due":                gorm.Expr("tiktok_comments.rules_due AND EXCLUDED.is_ours IS DISTINCT FROM true"),
			"updated_at":               gorm.Expr("now()"),
		}),
	}).CreateInBatches(records, upsertBatch).Error
}

func (r *commentRepository) ClaimLookups(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*ttdomain.Comment, error) {
	if limit <= 0 || limit > maxDueBatch {
		limit = maxDueBatch
	}
	var records []schema.TikTokComment
	if err := r.db.WithContext(ctx).Raw(claimCommentWorkSQL, now, limit, now.Add(lease)).Scan(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*ttdomain.Comment, len(records))
	for i := range records {
		out[i] = toCommentDomain(&records[i])
	}
	return out, nil
}

func (r *commentRepository) ResolveLookup(ctx context.Context, id string, a ttdomain.ResolvedAuthor) error {
	updates := map[string]any{
		"author_username":     strings.TrimSpace(a.Username),
		"author_display_name": strings.TrimSpace(a.DisplayName),
		"is_ours":             a.IsOurs,
		"liked_by_us":         a.Liked,
		"likes":               a.Likes,
		"replies":             a.Replies,
		"lookup":              string(ttdomain.LookupResolved),
		"lookup_attempts":     0,
		"next_lookup_at":      nil,
		"updated_at":          gorm.Expr("now()"),
	}
	if a.ContactID != nil {
		updates["contact_id"] = *a.ContactID
	}
	if a.UniqueIdentifier != "" {
		updates["author_unique_identifier"] = a.UniqueIdentifier
	}
	if a.Text != "" {
		updates["text"] = a.Text
	}
	if a.ImageURL != "" {
		updates["image_url"] = a.ImageURL
	}
	if a.Visibility != ttdomain.VisibilityUnknown {
		updates["visibility"] = gorm.Expr("CASE WHEN visibility = 'deleted' THEN visibility ELSE ? END", string(a.Visibility))
	}
	if a.CommentedAt != nil {
		updates["commented_at"] = *a.CommentedAt
	}
	if a.IsOurs {
		updates["rules_due"] = false
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(updates), ttdomain.ErrCommentNotFound)
}

func (r *commentRepository) DeferLookup(ctx context.Context, id string, attempts int, next time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"lookup_attempts": attempts,
		"next_lookup_at":  next,
	}), ttdomain.ErrCommentNotFound)
}

func (r *commentRepository) FailLookup(ctx context.Context, id string, attempts int) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"lookup":          string(ttdomain.LookupFailed),
		"lookup_attempts": attempts,
		"rules_due":       false,
		"next_lookup_at":  nil,
	}), ttdomain.ErrCommentNotFound)
}

func (r *commentRepository) MarkRulesDone(ctx context.Context, id string) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"rules_due":      false,
		"next_lookup_at": nil,
	}), ttdomain.ErrCommentNotFound)
}

func (r *commentRepository) FindByCommentID(ctx context.Context, accountID, commentID string) (*ttdomain.Comment, error) {
	var record schema.TikTokComment
	if err := r.db.WithContext(ctx).Where("account_id = ? AND comment_id = ?", accountID, commentID).Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, ttdomain.ErrCommentNotFound
	}
	return toCommentDomain(&record), nil
}

func (r *commentRepository) FindByCommentIDs(ctx context.Context, accountID string, commentIDs []string) ([]*ttdomain.Comment, error) {
	unique := shared.UniqueNonEmpty(commentIDs)
	out := make([]*ttdomain.Comment, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.TikTokComment
		if err := r.db.WithContext(ctx).Where("account_id = ? AND comment_id IN ?", accountID, unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toCommentDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *commentRepository) List(ctx context.Context, in ttdomain.ListCommentsInput) (*shared.PaginatedResult[*ttdomain.Comment], error) {
	if strings.TrimSpace(in.AccountID) == "" {
		return nil, ttdomain.ErrAccountNotFound
	}
	query := r.model(ctx).Where("account_id = ?", in.AccountID).Where("visibility <> ?", string(ttdomain.VisibilityDeleted))
	if in.VideoID != "" {
		query = query.Where("video_id = ?", in.VideoID)
	}
	if !in.IncludeHidden {
		query = query.Where("visibility <> ?", string(ttdomain.VisibilityHidden))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)
	var records []schema.TikTokComment
	if err := query.Order("commented_at DESC NULLS LAST").Order("comment_id DESC").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*ttdomain.Comment, len(records))
	for i := range records {
		out[i] = toCommentDomain(&records[i])
	}
	return shared.NewPaginatedResult(out, pagination, total), nil
}

func (r *commentRepository) SetVisibility(ctx context.Context, accountID, commentID string, visibility ttdomain.CommentVisibility, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("account_id = ? AND comment_id = ?", accountID, commentID).Updates(map[string]any{
		"visibility":    string(visibility),
		"last_event_at": gorm.Expr("GREATEST(COALESCE(last_event_at, ?), ?)", at, at),
	}), ttdomain.ErrCommentNotFound)
}

func (r *commentRepository) SetLiked(ctx context.Context, accountID, commentID string, liked bool) error {
	return database.AffectedOne(r.model(ctx).Where("account_id = ? AND comment_id = ?", accountID, commentID).Update("liked_by_us", liked), ttdomain.ErrCommentNotFound)
}
