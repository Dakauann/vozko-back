package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AIAttendanceSession tracks AI attendant spans for CRM metrics.
// EntryID and AgentID are text (not uuid): voice sessions often key entry by SIP
// Call-ID, and workflow actors may use workflow UUIDs or synthetic ids.
type AIAttendanceSession struct {
	ID                  string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID         string     `gorm:"type:uuid;not null;index:idx_ai_sess_ws_time,priority:1;index:idx_ai_sess_entry,priority:1"`
	EntryID             string     `gorm:"type:text;not null;index:idx_ai_sess_entry,priority:2;index:idx_ai_sess_open,priority:1"`
	EntryType           string     `gorm:"type:varchar(20);not null;index:idx_ai_sess_entry,priority:3;index:idx_ai_sess_open,priority:2"`
	AgentID             string     `gorm:"type:text;not null;index:idx_ai_sess_agent"`
	Channel             string     `gorm:"type:varchar(20);not null;default:''"`
	CallID              string     `gorm:"type:text;default:'';index:idx_ai_sess_call"`
	CampaignID          string     `gorm:"type:text;default:''"`
	StartedAt           time.Time  `gorm:"not null;index:idx_ai_sess_ws_time,priority:2"`
	EndedAt             *time.Time `gorm:"index:idx_ai_sess_open,priority:3,where:ended_at IS NULL"`
	Outcome             string     `gorm:"type:varchar(20);default:''"`
	HandoffTargetUserID string     `gorm:"type:text;default:''"`
	EndReason           string     `gorm:"type:varchar(64);default:''"`
	InboundMessageCount int        `gorm:"not null;default:0"`
	AIMessageCount      int        `gorm:"not null;default:0"`
	ToolCallCount       int        `gorm:"not null;default:0"`
	Model               string     `gorm:"type:varchar(120);default:''"`
	CreatedAt           time.Time  `gorm:"autoCreateTime"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime"`
}

func (AIAttendanceSession) TableName() string {
	return "ai_attendance_sessions"
}

func (s *AIAttendanceSession) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
