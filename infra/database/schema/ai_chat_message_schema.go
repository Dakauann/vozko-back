package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AIChatMessage is a single turn in an AIChatThread. Role is one of
// user|assistant|system|tool. ToolCalls holds the serialized tool calls/results
// for an assistant turn (JSONB). Token counts are stored for display/audit only —
// actual billing flows through the ai.billing.completed event keyed by RequestID.
type AIChatMessage struct {
	ID               string         `gorm:"primaryKey;type:text"`
	ThreadID         string         `gorm:"type:uuid;not null;index:idx_chat_msg_thread_created,priority:1"`
	Role             string         `gorm:"type:varchar(20);not null"`
	Content          string         `gorm:"type:text"`
	Model            string         `gorm:"type:text"`
	ToolCalls        []byte         `gorm:"type:jsonb"`
	Reasoning        []byte         `gorm:"type:jsonb"`
	PromptTokens     int            `gorm:"default:0"`
	CompletionTokens int            `gorm:"default:0"`
	CreatedAt        time.Time      `gorm:"autoCreateTime;index:idx_chat_msg_thread_created,priority:2"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
}

func (AIChatMessage) TableName() string {
	return "ai_chat_messages"
}

func (m *AIChatMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
