package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ReportJob struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_report_job_ws_created,priority:1;index:idx_report_job_reuse,priority:1"`
	RequestedBy string `gorm:"type:uuid;default:null"`

	Kind        string         `gorm:"type:varchar(64);not null;index:idx_report_job_reuse,priority:2"`
	Format      string         `gorm:"type:varchar(10);not null;default:'csv'"`
	Locale      string         `gorm:"type:varchar(10);not null;default:''"`
	Params      datatypes.JSON `gorm:"type:jsonb"`
	Fingerprint string         `gorm:"type:char(64);not null;default:'';index:idx_report_job_reuse,priority:3"`

	Status      string `gorm:"type:varchar(16);not null;default:'queued';index:idx_report_job_status"`
	Progress    int    `gorm:"not null;default:0"`
	FailureCode string `gorm:"type:varchar(64);not null;default:''"`

	ObjectKey string `gorm:"type:text;not null;default:''"`
	Filename  string `gorm:"type:text;not null;default:''"`
	SizeBytes int64  `gorm:"not null;default:0"`
	RowCount  int64  `gorm:"not null;default:0"`

	ExpiresAt  *time.Time `gorm:"index:idx_report_job_expires"`
	StartedAt  *time.Time
	FinishedAt *time.Time

	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_report_job_ws_created,priority:2"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (ReportJob) TableName() string {
	return "report_jobs"
}

func (j *ReportJob) BeforeCreate(tx *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	return nil
}
