package comment_analysis_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	ca "vozko/domain/comment_analysis"
	"vozko/infra/database/schema"
)

// The read the rest of the system makes about conversations.
//
// Separate from the engine's repository on purpose: these callers need two
// reads and have no business holding the queue, the claim or the purge. It is
// the same table and the same mapper, so there is no second definition of what
// an analysis is.

type conversationReader struct {
	db *gorm.DB
}

func NewConversationReader(db *gorm.DB) ca.ConversationReader {
	return &conversationReader{db: db}
}

// LatestByEntries answers for many conversations in ONE query.
//
// The previous engine's equivalent ran a query per entry from a loop in the
// websocket path, which is what made opening the inbox slow in proportion to
// how many conversations were on screen.
func (r *conversationReader) LatestByEntries(
	ctx context.Context, workspaceID string, source ca.Source, entryIDs []string,
) (map[string]*ca.CommentAnalysis, error) {
	out := map[string]*ca.CommentAnalysis{}
	if len(entryIDs) == 0 {
		return out, nil
	}

	var rows []schema.CommentAnalysis
	q := r.db.WithContext(ctx).
		Where("subject_kind = ? AND source_comment_id IN ? AND deleted_at IS NULL",
			string(ca.SubjectKindConversation), entryIDs)
	// Both scopes are optional so a caller that legitimately has neither (a
	// background reconciler) is not forced to invent one, but a workspace is
	// applied whenever it is known: these rows carry customer prose.
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if source != "" {
		q = q.Where("source = ?", string(source))
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}

	for i := range rows {
		// One row per conversation by construction (the unique index), so the
		// last write wins rather than an arbitrary one of several.
		out[rows[i].SourceCommentID] = toDomain(&rows[i])
	}
	return out, nil
}

func (r *conversationReader) LatestByEntry(
	ctx context.Context, workspaceID string, source ca.Source, entryID string,
) (*ca.CommentAnalysis, error) {
	var row schema.CommentAnalysis
	q := r.db.WithContext(ctx).
		Where("subject_kind = ? AND source_comment_id = ? AND deleted_at IS NULL",
			string(ca.SubjectKindConversation), entryID)
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if source != "" {
		q = q.Where("source = ?", string(source))
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}
