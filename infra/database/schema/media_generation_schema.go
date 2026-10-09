package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MediaGenerationJob struct {
	ID                string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string         `gorm:"type:uuid;not null;uniqueIndex:idx_media_generation_jobs_active,priority:1,where:status = 'queued' OR status = 'running'"`
	RequestedBy       string         `gorm:"type:uuid;not null;uniqueIndex:idx_media_generation_jobs_active,priority:2"`
	Fingerprint       string         `gorm:"size:64;not null;uniqueIndex:idx_media_generation_jobs_active,priority:3"`
	Kind              string         `gorm:"size:16;not null;default:'image'"`
	Voice             string         `gorm:"size:32"`
	Video             datatypes.JSON `gorm:"type:jsonb"`
	SourceMediaID     string         `gorm:"size:64"`
	BillingReference  string         `gorm:"size:128"`
	Prompt            string         `gorm:"type:text;not null"`
	Aspect            string         `gorm:"size:16;not null"`
	ReferenceMediaIDs pq.StringArray `gorm:"type:text[]"`
	Status            string         `gorm:"size:16;not null;index:idx_media_generation_jobs_stale,priority:1"`
	MediaID           string         `gorm:"size:64"`
	MediaURL          string         `gorm:"type:text"`
	Model             string         `gorm:"size:128"`
	GenerationID      string         `gorm:"size:128"`
	FailureCode       string         `gorm:"size:32"`
	FailureDetail     string         `gorm:"type:text"`
	Attempts          int            `gorm:"not null;default:0"`
	CreatedAt         time.Time      `gorm:"autoCreateTime;index:idx_media_generation_jobs_stale,priority:2"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime"`
	FinishedAt        *time.Time     `gorm:"type:timestamptz"`
}

func (MediaGenerationJob) TableName() string { return "media_generation_jobs" }

func (j *MediaGenerationJob) BeforeCreate(tx *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	return nil
}
