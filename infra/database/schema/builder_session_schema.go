package schema

import "time"

type BuilderSessionSchema struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null;index"`
	WorkflowID  *string   `gorm:"type:uuid;index"`
	Mode        string    `gorm:"size:20"`
	Model       string    `gorm:"size:120"`
	Title       string    `gorm:"type:text"`
	Valid       bool      `gorm:"not null;default:false"`
	StartedAt   time.Time `gorm:"index"`
	EndedAt     *time.Time

	Messages []BuilderMessageSchema `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
}

func (BuilderSessionSchema) TableName() string {
	return "workflow_builder_sessions"
}

type BuilderMessageSchema struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	SessionID string `gorm:"type:uuid;not null;index"`
	Role      string `gorm:"size:16;not null"`
	Text      string `gorm:"type:text"`
	Tool      string `gorm:"size:120"`
	Ok        *bool
	CreatedAt time.Time
}

func (BuilderMessageSchema) TableName() string {
	return "workflow_builder_messages"
}
