package schema

import "time"

type AIUsageRecord struct {
	ReferenceID      string    `gorm:"primaryKey;type:varchar(255);index:idx_ai_usage_ws_reference,priority:2"`
	WorkspaceID      string    `gorm:"not null;type:uuid;index:idx_ai_usage_ws_reference,priority:1"`
	Model            string    `gorm:"type:text"`
	InputTokens      int64     `gorm:"not null;default:0"`
	OutputTokens     int64     `gorm:"not null;default:0"`
	CacheReadTokens  int64     `gorm:"not null;default:0"`
	CacheWriteTokens int64     `gorm:"not null;default:0"`
	ReasoningTokens  int64     `gorm:"not null;default:0"`
	Billed           bool      `gorm:"not null;default:false"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
}

func (AIUsageRecord) TableName() string {
	return "ai_usage_records"
}
