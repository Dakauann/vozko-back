package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AudienceAnalysis struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_workspace"`

	SubjectKind string `gorm:"size:16;not null;default:'comment'"`
	Revision    string `gorm:"size:64;not null;default:''"`
	Transcript  string `gorm:"type:text;not null;default:''"`

	Source          string  `gorm:"size:32;not null"`
	AccountID       string  `gorm:"type:uuid;not null;index:idx_ca_account"`
	ContainerID     string  `gorm:"size:64;not null"`
	SubjectID       string  `gorm:"size:64;not null"`
	ParentSubjectID *string `gorm:"size:64"`

	AuthorExternalID string `gorm:"size:64;not null"`
	AuthorHandle     string `gorm:"size:64"`

	Status        string `gorm:"size:16;not null;default:'pending'"`
	Attempts      int    `gorm:"not null;default:0"`
	FailureReason string `gorm:"size:200"`

	Sentiment string `gorm:"size:16"`
	Stance    string `gorm:"size:16"`
	Intent    string `gorm:"size:24"`
	TopicKey  string `gorm:"size:64"`
	IsSpam    bool   `gorm:"not null;default:false"`
	Language  string `gorm:"size:16"`

	Toxicity       string `gorm:"size:8"`
	PersonalAttack string `gorm:"size:8"`
	LegalRisk      string `gorm:"size:8"`
	Severity       int    `gorm:"not null;default:0"`

	Interest           string `gorm:"size:24"`
	ProductInterest    string `gorm:"size:160"`
	ProductInterestKey string `gorm:"size:64;not null;default:''"`
	Disposition        string `gorm:"size:24"`
	Qualification      string `gorm:"size:16"`
	NextAction         string `gorm:"size:24"`
	Summary            string `gorm:"type:text"`
	AttendanceQuality  int    `gorm:"not null;default:0"`
	MessageCount       int    `gorm:"not null;default:0"`

	RequiresAction bool   `gorm:"not null;default:false"`
	Excerpt        string `gorm:"size:800"`
	Truncated      bool   `gorm:"not null;default:false"`

	BatchID    *string    `gorm:"type:uuid"`
	Model      string     `gorm:"size:120"`
	AnalyzedAt *time.Time `gorm:"type:timestamptz"`

	OccurredAt time.Time  `gorm:"type:timestamptz;not null;index:idx_ca_commented"`
	CreatedAt  time.Time  `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt  time.Time  `gorm:"autoUpdateTime;type:timestamptz"`
	DeletedAt  *time.Time `gorm:"type:timestamptz"`
}

func (AudienceAnalysis) TableName() string { return "audience_analyses" }

func (c *AudienceAnalysis) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type AudienceSettings struct {
	Source      string `gorm:"primaryKey;size:32"`
	AccountID   string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	Enabled              bool           `gorm:"not null;default:false"`
	Model                string         `gorm:"size:120"`
	Vertical             string         `gorm:"size:16;not null;default:'services'"`
	Topics               datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	SeverityThreshold    int            `gorm:"not null;default:60"`
	DailyCap             int            `gorm:"not null;default:20000"`
	Instructions         string         `gorm:"type:text"`
	ReplyMode            string         `gorm:"size:16;not null;default:'off'"`
	ReplyMaxAutoSeverity int            `gorm:"not null;default:30"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (AudienceSettings) TableName() string { return "audience_settings" }

type AudienceAuthor struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null"`

	AuthorExternalID string `gorm:"size:64;not null"`
	AuthorHandle     string `gorm:"size:64"`

	FirstSeenAt time.Time `gorm:"type:timestamptz"`
	LastSeenAt  time.Time `gorm:"type:timestamptz"`

	Counters        datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	TotalComments   int            `gorm:"not null;default:0"`
	MaxSeverity     int            `gorm:"not null;default:0"`
	HighSevCount    int            `gorm:"not null;default:0"`
	StanceHostile   int            `gorm:"not null;default:0"`
	StanceSupporter int            `gorm:"not null;default:0"`
	Reputation      int            `gorm:"not null;default:0;index"`
	TopTopics       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	DerivedStance   string         `gorm:"size:16;not null;default:'neutral'"`
	IsFlagged       bool           `gorm:"not null;default:false"`
	ModerationState string         `gorm:"size:16;not null;default:'none'"`
	Role            string         `gorm:"size:24;not null;default:'unknown'"`
	RoleConfidence  string         `gorm:"size:8;not null;default:'none'"`
	RoleComments    int            `gorm:"not null;default:0"`
	RoleRationale   string         `gorm:"size:220"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (AudienceAuthor) TableName() string { return "audience_authors" }

func (a *AudienceAuthor) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type AudienceRollup struct {
	WorkspaceID string    `gorm:"type:uuid;not null;index"`
	Source      string    `gorm:"primaryKey;size:32"`
	AccountID   string    `gorm:"type:uuid;not null"`
	Scope       string    `gorm:"primaryKey;size:16"`
	ScopeID     string    `gorm:"primaryKey;size:128"`
	BucketDate  time.Time `gorm:"primaryKey;type:date"`

	Counters        datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	AcceptanceScore int            `gorm:"not null;default:0"`
	ComputedAt      time.Time      `gorm:"type:timestamptz"`
}

func (AudienceRollup) TableName() string { return "audience_rollups" }

type AudienceBatch struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_batch_ws,priority:1"`
	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null"`
	ContainerID string `gorm:"size:64;not null"`

	Kind             string `gorm:"size:16;not null;default:'comment'"`
	Model            string `gorm:"size:120"`
	ItemCount        int    `gorm:"not null;default:0"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	PriceMicros      int64  `gorm:"not null;default:0"`
	Outcome          string `gorm:"size:16;not null"`
	RequestID        string `gorm:"size:128"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz;index:idx_ca_batch_ws,priority:2"`
}

func (AudienceBatch) TableName() string { return "audience_batches" }

type AudienceBackfill struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null;index"`
	ContainerID string `gorm:"size:64;not null;default:''"`

	Status string `gorm:"size:16;not null;default:'pending';index"`
	Cursor string `gorm:"type:text"`

	EstimatedComments int    `gorm:"not null;default:0"`
	Fetched           int    `gorm:"not null;default:0"`
	Enqueued          int    `gorm:"not null;default:0"`
	Error             string `gorm:"size:500"`

	RequestedByUserID string     `gorm:"type:uuid"`
	CreatedAt         time.Time  `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime;type:timestamptz"`
	FinishedAt        *time.Time `gorm:"type:timestamptz"`
}

func (AudienceBackfill) TableName() string { return "audience_backfills" }

func (b *AudienceBackfill) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

type AudienceContainerSettings struct {
	Source      string `gorm:"primaryKey;size:32"`
	AccountID   string `gorm:"primaryKey;type:uuid"`
	ContainerID string `gorm:"primaryKey;size:64"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	Enabled           *bool          `gorm:"type:boolean"`
	Model             *string        `gorm:"size:120"`
	Topics            datatypes.JSON `gorm:"type:jsonb"`
	SeverityThreshold *int           `gorm:"type:integer"`
	Instructions      *string        `gorm:"type:text"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (AudienceContainerSettings) TableName() string {
	return "audience_container_settings"
}

type AudienceAlertRule struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_alert_ws"`
	Source      string `gorm:"size:32;not null;index:idx_ca_alert_armed,priority:1"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_ca_alert_armed,priority:2"`

	Name            string  `gorm:"size:80;not null"`
	Enabled         bool    `gorm:"not null;default:false;index:idx_ca_alert_armed,priority:3"`
	CreatedByUserID *string `gorm:"type:uuid"`

	Metric        string `gorm:"size:32;not null"`
	Threshold     int    `gorm:"not null;default:0"`
	WindowMinutes int    `gorm:"not null;default:0"`
	MinMessages   int    `gorm:"not null;default:0"`

	Channel         string  `gorm:"size:16;not null"`
	Recipient       string  `gorm:"size:32;not null"`
	BusinessPhoneID *string `gorm:"type:uuid"`
	TemplateID      *string `gorm:"type:uuid"`
	InstanceID      *string `gorm:"type:uuid"`

	Brief bool `gorm:"not null;default:false"`

	CooldownMinutes int `gorm:"not null;default:60"`
	MaxPerDay       int `gorm:"not null;default:6"`

	LastFiredAt *time.Time `gorm:"type:timestamptz"`
	FiredToday  int        `gorm:"not null;default:0"`
	FiredDay    string     `gorm:"size:10;not null;default:''"`
	LastError   string     `gorm:"size:300;not null;default:''"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (AudienceAlertRule) TableName() string { return "audience_alert_rules" }

func (r *AudienceAlertRule) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}
