package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallRecording struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	CallID       string         `gorm:"size:255;not null;index"`
	CallRecordID *string        `gorm:"type:uuid;index"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index"`
	EntryID      *string        `gorm:"type:uuid;index"`
	LeadID       *string        `gorm:"type:uuid;index"`
	RecordingURL string         `gorm:"size:1000;not null"`
	DurationSec  int            `gorm:"not null;default:0"`
	FileSize     int64          `gorm:"not null;default:0"`
	CallStart    time.Time      `gorm:"not null"`
	CallEnd      time.Time      `gorm:"not null"`
	CreatedAt    time.Time      `gorm:"autoCreateTime;index"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (CallRecording) TableName() string {
	return "call_recordings"
}

func (cr *CallRecording) BeforeCreate(tx *gorm.DB) error {
	if cr.ID == "" {
		cr.ID = uuid.New().String()
	}
	return nil
}
