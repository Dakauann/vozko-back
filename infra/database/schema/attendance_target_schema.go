package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AttendanceTarget struct {
	ID          string    `gorm:"primaryKey;type:text"`
	WorkspaceID string    `gorm:"type:uuid;not null;uniqueIndex:idx_att_target_unique,priority:1;index:idx_att_target_ws_period,priority:1"`
	Scope       string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_att_target_unique,priority:2"`
	ScopeID     string    `gorm:"type:text;not null;default:'';uniqueIndex:idx_att_target_unique,priority:3"`
	MetricKey   string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_att_target_unique,priority:4"`
	PeriodStart time.Time `gorm:"type:date;not null;uniqueIndex:idx_att_target_unique,priority:5;index:idx_att_target_ws_period,priority:2"`
	Value       float64   `gorm:"not null;default:0"`
	Currency    string    `gorm:"type:varchar(3);not null;default:''"`
	CreatedBy   string    `gorm:"type:text;not null;default:''"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (AttendanceTarget) TableName() string {
	return "attendance_targets"
}

func (t *AttendanceTarget) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
