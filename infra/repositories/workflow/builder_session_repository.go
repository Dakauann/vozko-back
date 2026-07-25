package workflow_repository

import (
	"context"
	"errors"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type builderSessionRepository struct {
	db *gorm.DB
}

func NewBuilderSessionRepository(db *gorm.DB) workflow.BuilderSessionRepository {
	return &builderSessionRepository{db: db}
}

func (r *builderSessionRepository) CreateSession(ctx context.Context, s *workflow.BuilderSession) error {
	row := schema.BuilderSessionSchema{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		WorkflowID:  nilIfEmpty(s.WorkflowID),
		Mode:        s.Mode,
		Model:       s.Model,
		Title:       s.Title,
		Valid:       s.Valid,
		StartedAt:   s.StartedAt,
		EndedAt:     s.EndedAt,
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *builderSessionRepository) UpdateSession(ctx context.Context, s *workflow.BuilderSession) error {
	return r.db.WithContext(ctx).
		Model(&schema.BuilderSessionSchema{}).
		Where("id = ?", s.ID).
		Updates(map[string]interface{}{
			"title":    s.Title,
			"valid":    s.Valid,
			"ended_at": s.EndedAt,
		}).Error
}

func (r *builderSessionRepository) AppendMessage(ctx context.Context, sessionID string, m workflow.BuilderMessage) error {
	row := schema.BuilderMessageSchema{
		SessionID: sessionID,
		Role:      string(m.Role),
		Text:      m.Text,
		Tool:      m.Tool,
		Ok:        m.Ok,
		CreatedAt: m.At,
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *builderSessionRepository) ListByWorkflow(ctx context.Context, workspaceID, workflowID string) ([]*workflow.BuilderSession, error) {
	return r.listWhere(ctx, "workspace_id = ? AND workflow_id = ?", []interface{}{workspaceID, workflowID}, 0)
}

func (r *builderSessionRepository) ListByWorkspace(ctx context.Context, workspaceID string, limit int) ([]*workflow.BuilderSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return r.listWhere(ctx, "workspace_id = ?", []interface{}{workspaceID}, limit)
}

func (r *builderSessionRepository) listWhere(ctx context.Context, where string, args []interface{}, limit int) ([]*workflow.BuilderSession, error) {
	q := r.db.WithContext(ctx).
		Model(&schema.BuilderSessionSchema{}).
		Where(where, args...).
		Order("started_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []schema.BuilderSessionSchema
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	counts, err := r.messageCounts(ctx, sessionIDs(rows))
	if err != nil {
		return nil, err
	}
	out := make([]*workflow.BuilderSession, 0, len(rows))
	for i := range rows {
		s := mapBuilderSession(rows[i])
		s.MessageCount = counts[s.ID]
		out = append(out, s)
	}
	return out, nil
}

func (r *builderSessionRepository) GetByID(ctx context.Context, workspaceID, id string) (*workflow.BuilderSession, error) {
	var row schema.BuilderSessionSchema
	err := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var msgs []schema.BuilderMessageSchema
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", id).
		Order("id ASC").
		Find(&msgs).Error; err != nil {
		return nil, err
	}

	session := mapBuilderSession(row)
	session.Messages = make([]workflow.BuilderMessage, 0, len(msgs))
	for _, m := range msgs {
		session.Messages = append(session.Messages, workflow.BuilderMessage{
			Role: workflow.BuilderMessageRole(m.Role),
			Text: m.Text,
			Tool: m.Tool,
			Ok:   m.Ok,
			At:   m.CreatedAt,
		})
	}
	session.MessageCount = len(session.Messages)
	return session, nil
}

func (r *builderSessionRepository) messageCounts(ctx context.Context, sessionIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return counts, nil
	}
	var rows []struct {
		SessionID string
		Count     int
	}
	if err := r.db.WithContext(ctx).
		Model(&schema.BuilderMessageSchema{}).
		Select("session_id, COUNT(*) AS count").
		Where("session_id IN ?", sessionIDs).
		Group("session_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.SessionID] = row.Count
	}
	return counts, nil
}

func sessionIDs(rows []schema.BuilderSessionSchema) []string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	return ids
}

func mapBuilderSession(row schema.BuilderSessionSchema) *workflow.BuilderSession {
	workflowID := ""
	if row.WorkflowID != nil {
		workflowID = *row.WorkflowID
	}
	return &workflow.BuilderSession{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		WorkflowID:  workflowID,
		Mode:        row.Mode,
		Model:       row.Model,
		Title:       row.Title,
		Valid:       row.Valid,
		StartedAt:   row.StartedAt,
		EndedAt:     row.EndedAt,
	}
}
