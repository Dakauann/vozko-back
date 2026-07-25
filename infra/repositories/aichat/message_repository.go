package aichat_repository

import (
	"gorm.io/gorm"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
)

type messageRepository struct {
	db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) aichat.MessageRepository {
	return &messageRepository{db: db}
}

func (r *messageRepository) Create(message *aichat.Message) error {
	rec := toMessageSchema(message)
	if err := r.db.Create(rec).Error; err != nil {
		return err
	}
	message.ID = rec.ID
	message.CreatedAt = rec.CreatedAt
	return nil
}

func (r *messageRepository) ListByThread(input aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	q := r.db.Model(&schema.AIChatMessage{}).Where("thread_id = ?", input.ThreadID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var recs []schema.AIChatMessage
	err := q.Order("created_at ASC").
		Limit(input.Limit).
		Offset(input.Offset).
		Find(&recs).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]*aichat.Message, len(recs))
	for i := range recs {
		out[i] = toMessageDomain(&recs[i])
	}
	return out, total, nil
}

func (r *messageRepository) DeleteByThread(threadID string) error {
	return r.db.Where("thread_id = ?", threadID).Delete(&schema.AIChatMessage{}).Error
}

func toMessageDomain(rec *schema.AIChatMessage) *aichat.Message {
	return &aichat.Message{
		ID:               rec.ID,
		ThreadID:         rec.ThreadID,
		Role:             aichat.Role(rec.Role),
		Content:          rec.Content,
		Model:            rec.Model,
		ToolCalls:        rec.ToolCalls,
		Reasoning:        rec.Reasoning,
		PromptTokens:     rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens,
		CreatedAt:        rec.CreatedAt,
	}
}

func toMessageSchema(m *aichat.Message) *schema.AIChatMessage {
	return &schema.AIChatMessage{
		ID:               m.ID,
		ThreadID:         m.ThreadID,
		Role:             string(m.Role),
		Content:          m.Content,
		Model:            m.Model,
		ToolCalls:        m.ToolCalls,
		Reasoning:        m.Reasoning,
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
	}
}
