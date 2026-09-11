package audience_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
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
) (map[string]*ca.Analysis, error) {
	out := map[string]*ca.Analysis{}
	if len(entryIDs) == 0 {
		return out, nil
	}

	var rows []schema.AudienceAnalysis
	q := r.db.WithContext(ctx).
		Where("subject_kind = ? AND subject_id IN ? AND deleted_at IS NULL AND status = 'analyzed'",
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
	if err := q.Select("DISTINCT ON (source, subject_id) *").
		Order("source, subject_id, occurred_at DESC, created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}

	for i := range rows {
		// SQL selected the latest completed revision; pending work never erases
		// the last available verdict on the inbox.
		out[rows[i].SubjectID] = toDomain(&rows[i])
	}
	return out, nil
}

func (r *conversationReader) LatestByEntry(
	ctx context.Context, workspaceID string, source ca.Source, entryID string,
) (*ca.Analysis, error) {
	var row schema.AudienceAnalysis
	q := r.db.WithContext(ctx).
		Where("subject_kind = ? AND subject_id = ? AND deleted_at IS NULL AND status = 'analyzed'",
			string(ca.SubjectKindConversation), entryID)
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if source != "" {
		q = q.Where("source = ?", string(source))
	}
	if err := q.Order("occurred_at DESC, created_at DESC, id DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

// PendingByEntries is ONE indexed read for a whole inbox page.
//
// Deliberately narrow: it selects the subject ids and nothing else, so the
// answer is a handful of strings however large the transcripts behind them are.
// The inbox calls it beside LatestByEntries on the same page of entries, which
// is why it must not become a per-entry query, and why it returns only the ids
// that actually have work waiting.
func (r *conversationReader) PendingByEntries(
	ctx context.Context, workspaceID string, source ca.Source, entryIDs []string,
) (map[string]bool, error) {
	out := map[string]bool{}
	if len(entryIDs) == 0 {
		return out, nil
	}

	var ids []string
	q := r.db.WithContext(ctx).
		Model(&schema.AudienceAnalysis{}).
		Distinct("subject_id").
		Where("subject_kind = ? AND subject_id IN ? AND deleted_at IS NULL AND status IN ?",
			string(ca.SubjectKindConversation), entryIDs,
			[]string{string(ca.StatusPending), string(ca.StatusInFlight)})
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if source != "" {
		q = q.Where("source = ?", string(source))
	}
	if err := q.Pluck("subject_id", &ids).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
