package schema

import "time"

type ChoicePrompt struct {
	PromptKey         string    `gorm:"type:text;primaryKey"`
	WorkspaceID       string    `gorm:"type:uuid;not null"`
	SourceKind        string    `gorm:"type:varchar(32);not null"`
	SourceID          string    `gorm:"type:text;not null;default:''"`
	NodeID            string    `gorm:"type:text;not null;default:''"`
	Version           int       `gorm:"not null;default:0"`
	TemplateID        string    `gorm:"type:text;not null;default:''"`
	ProviderMessageID string    `gorm:"type:text;not null;default:''"`
	SentDay           time.Time `gorm:"type:date;not null"`
	SentAt            time.Time `gorm:"not null"`
	Shard             int16     `gorm:"not null;default:0"`
	ClickedOptionID   string    `gorm:"type:text;not null;default:''"`
	ClickedAt         *time.Time
}

func (ChoicePrompt) TableName() string { return "choice_prompts" }

type ChoiceDailyCount struct {
	WorkspaceID string    `gorm:"type:uuid;primaryKey"`
	SourceKind  string    `gorm:"type:varchar(32);primaryKey"`
	SourceID    string    `gorm:"type:text;primaryKey"`
	NodeID      string    `gorm:"type:text;primaryKey"`
	Version     int       `gorm:"primaryKey;autoIncrement:false"`
	OptionID    string    `gorm:"type:text;primaryKey"`
	Day         time.Time `gorm:"type:date;primaryKey"`
	Shard       int16     `gorm:"primaryKey;autoIncrement:false"`
	TemplateID  string    `gorm:"type:text;not null;default:''"`
	Shown       int64     `gorm:"not null;default:0"`
	Clicks      int64     `gorm:"not null;default:0"`
}

func (ChoiceDailyCount) TableName() string { return "choice_daily_counts" }

func ChoiceStatsModels() []any {
	return []any{&ChoicePrompt{}, &ChoiceDailyCount{}}
}
