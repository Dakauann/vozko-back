package schema

import "time"

type ConversationDelegation struct {
	EntryID        string    `gorm:"primaryKey;type:text"`
	EntryType      string    `gorm:"primaryKey;size:32"`
	WorkspaceID    string    `gorm:"type:uuid;not null;index"`
	AutomationKind string    `gorm:"size:16;not null"`
	AutomationID   string    `gorm:"type:text;not null"`
	DelegatedBy    string    `gorm:"type:text;not null"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

func (ConversationDelegation) TableName() string { return "conversation_delegations" }
