package aichat_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
)

type threadRepository struct {
	db *gorm.DB
}

func NewThreadRepository(db *gorm.DB) aichat.ThreadRepository {
	return &threadRepository{db: db}
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

func (r *threadRepository) Touch(id string, lastMessageAt time.Time, model string) error {
	updates := map[string]any{"last_message_at": lastMessageAt}
	if model != "" {
		updates["model"] = model
	}
	return r.db.Model(&schema.AIChatThread{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *threadRepository) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&schema.AIChatThread{}).Error
}

func toThreadDomain(rec *schema.AIChatThread) *aichat.Thread {
	return &aichat.Thread{
		ID:            rec.ID,
		WorkspaceID:   rec.WorkspaceID,
		UserID:        rec.UserID,
		Title:         rec.Title,
		Model:         rec.Model,
		LastMessageAt: rec.LastMessageAt,
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}

func toThreadSchema(t *aichat.Thread) *schema.AIChatThread {
	return &schema.AIChatThread{
		ID:            t.ID,
		WorkspaceID:   t.WorkspaceID,
		UserID:        t.UserID,
		Title:         t.Title,
		Model:         t.Model,
		LastMessageAt: t.LastMessageAt,
	}
}
