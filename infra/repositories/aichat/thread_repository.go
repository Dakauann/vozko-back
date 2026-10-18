package aichat_repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

type threadRepository struct {
	db *gorm.DB
}

func NewThreadRepository(db *gorm.DB) aichat.ThreadRepository {
	return &threadRepository{db: db}
}

func NewThreadProjects(db *gorm.DB) aichat.ThreadProjects {
	return &threadRepository{db: db}
}

func NewProjectAdopter(db *gorm.DB) aichat.ProjectAdopter {
	return &threadRepository{db: db}
}

func (r *threadRepository) AdoptProject(threadID, projectID string) error {
	res := r.db.Model(&schema.AIChatThread{}).Where("id = ?", threadID).Update("project_id", projectID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return aichat.ErrThreadNotFound
	}
	return nil
}

func (r *threadRepository) BindProject(threadID, projectID string) (bool, error) {
	res := r.db.Model(&schema.AIChatThread{}).
		Where("id = ? AND (project_id IS NULL OR project_id = ?)", threadID, projectID).
		Update("project_id", projectID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *threadRepository) Create(thread *aichat.Thread) error {
	rec := toThreadSchema(thread)
	if err := r.db.Create(rec).Error; err != nil {
		return err
	}
	thread.ID = rec.ID
	thread.CreatedAt = rec.CreatedAt
	thread.UpdatedAt = rec.UpdatedAt
	return nil
}

func (r *threadRepository) GetByID(id string) (*aichat.Thread, error) {
	var rec schema.AIChatThread
	if err := r.db.Where("id = ?", id).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, aichat.ErrThreadNotFound
		}
		return nil, err
	}
	return toThreadDomain(&rec), nil
}

func (r *threadRepository) ListByUser(input aichat.ListThreadsInput) ([]*aichat.Thread, int64, error) {
	q := r.db.Model(&schema.AIChatThread{}).
		Where("workspace_id = ? AND user_id = ?", input.WorkspaceID, input.UserID)
	if search := strings.TrimSpace(input.Query); search != "" {
		q = q.Where(`title ILIKE ? ESCAPE '\'`, sqlexpr.LikeContains(search))
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var recs []schema.AIChatThread
	err := q.Order("COALESCE(last_message_at, created_at) DESC").
		Limit(input.Limit).
		Offset(input.Offset).
		Find(&recs).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]*aichat.Thread, len(recs))
	for i := range recs {
		out[i] = toThreadDomain(&recs[i])
	}
	return out, total, nil
}

func (r *threadRepository) Rename(id, title string) error {
	return r.db.Model(&schema.AIChatThread{}).
		Where("id = ?", id).
		Update("title", title).Error
}

func (r *threadRepository) Touch(id string, lastMessageAt time.Time, model, reasoningLevel string) error {
	updates := map[string]any{"last_message_at": lastMessageAt}
	if model != "" {
		updates["model"] = model
		updates["reasoning_level"] = reasoningLevel
	}
	return r.db.Model(&schema.AIChatThread{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *threadRepository) SaveCheckpoint(id, text string, at time.Time) error {
	return r.db.Model(&schema.AIChatThread{}).
		Where("id = ?", id).
		Updates(map[string]any{"checkpoint": text, "checkpoint_at": at}).Error
}

func (r *threadRepository) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&schema.AIChatThread{}).Error
}

func toThreadDomain(rec *schema.AIChatThread) *aichat.Thread {
	return &aichat.Thread{
		ID:             rec.ID,
		WorkspaceID:    rec.WorkspaceID,
		UserID:         rec.UserID,
		Title:          rec.Title,
		Model:          rec.Model,
		ReasoningLevel: rec.ReasoningLevel,
		ProjectID:      projectOf(rec.ProjectID),
		LastMessageAt:  rec.LastMessageAt,
		CostTracked:    rec.CostTracked,
		Checkpoint:     rec.Checkpoint,
		CheckpointAt:   rec.CheckpointAt,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

func projectOf(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func projectColumn(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func toThreadSchema(t *aichat.Thread) *schema.AIChatThread {
	return &schema.AIChatThread{
		ID:             t.ID,
		WorkspaceID:    t.WorkspaceID,
		UserID:         t.UserID,
		Title:          t.Title,
		Model:          t.Model,
		ReasoningLevel: t.ReasoningLevel,
		ProjectID:      projectColumn(t.ProjectID),
		LastMessageAt:  t.LastMessageAt,
		CostTracked:    t.CostTracked,
	}
}
