package audience_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
)

type conversationReader struct {
	db *gorm.DB
}

func NewConversationReader(db *gorm.DB) ca.ConversationReader {
	return &conversationReader{db: db}
}

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
