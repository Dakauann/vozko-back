package schema

import (
	"time"

	"gorm.io/datatypes"
)

type LiveDecisionRecord struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index:idx_ldr_ws_created,priority:1"`
	EntryID       string         `gorm:"type:varchar(64);not null;default:''"`
	EntryType     string         `gorm:"type:varchar(30);not null;default:''"`
	Purpose       string         `gorm:"type:varchar(20);not null"`
	Answers       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Effects       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Failure       string         `gorm:"type:varchar(500);not null;default:''"`
	Model         string         `gorm:"type:varchar(120);not null;default:''"`
	InputTokens   int            `gorm:"not null;default:0"`
	CostMicros    int64          `gorm:"not null;default:0"`
	LatencyMillis int64          `gorm:"not null;default:0"`
	CreatedAt     time.Time      `gorm:"not null;index:idx_ldr_created;index:idx_ldr_ws_created,priority:2"`
}

func (LiveDecisionRecord) TableName() string { return "live_decision_records" }

type ConversationLiveRead struct {
	EntryID           string         `gorm:"primaryKey;type:varchar(64)"`
	EntryType         string         `gorm:"primaryKey;type:varchar(30)"`
	WorkspaceID       string         `gorm:"type:uuid;not null;index"`
	Interest          string         `gorm:"type:varchar(30);not null;default:''"`
	Disposition       string         `gorm:"type:varchar(30);not null;default:''"`
	Sentiment         string         `gorm:"type:varchar(30);not null;default:''"`
	Qualification     string         `gorm:"type:varchar(30);not null;default:''"`
	NextAction        string         `gorm:"type:varchar(30);not null;default:''"`
	Language          string         `gorm:"type:varchar(10);not null;default:''"`
	AttendanceQuality int            `gorm:"not null;default:0"`
	Certainty         datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	StageSettled      bool           `gorm:"not null;default:false"`
	DecidedThrough    time.Time      `gorm:"not null"`
	DecidedAt         time.Time      `gorm:"not null"`
}

func (ConversationLiveRead) TableName() string { return "conversation_live_reads" }
