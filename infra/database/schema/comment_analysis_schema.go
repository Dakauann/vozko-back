package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Comment analysis: the channel-neutral engine that classifies public
// comments on a customer's posts (INSTAGRAM_COMMENT_ANALYSIS_PLAN.md).
//
// Five tables. comment_analyses is the QUEUE as well as the result store:
// a row is inserted pending at ingest and the database, not Redis, says what
// is waiting. The partial indexes that make the queue cheap (pending by
// container, stale in_flight, the unique (source, source_comment_id) that
// makes webhook redelivery free) are declared in indexes.go because GORM
// tags cannot express a WHERE clause.

// CommentAnalysis is one classified comment. Comment TEXT is never stored
// here (only a 200-rune excerpt): it lives in the channel's own table and
// inherits that table's retention and PII posture.
type CommentAnalysis struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_workspace"`

	Source          string  `gorm:"size:32;not null"`
	AccountID       string  `gorm:"type:uuid;not null;index:idx_ca_account"`
	ContainerID     string  `gorm:"size:64;not null"`
	SourceCommentID string  `gorm:"size:64;not null"`
	ParentCommentID *string `gorm:"size:64"`

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

	RequiresAction bool   `gorm:"not null;default:false"`
	Excerpt        string `gorm:"size:800"`
	Truncated      bool   `gorm:"not null;default:false"`

	BatchID    *string    `gorm:"type:uuid"`
	Model      string     `gorm:"size:120"`
	AnalyzedAt *time.Time `gorm:"type:timestamptz"`

	// CommentedAt is the channel's timestamp; rollups bucket by it.
	CommentedAt time.Time  `gorm:"type:timestamptz;not null;index:idx_ca_commented"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime;type:timestamptz"`
	DeletedAt   *time.Time `gorm:"type:timestamptz"`
}

func (CommentAnalysis) TableName() string { return "comment_analyses" }

func (c *CommentAnalysis) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// CommentAnalysisSettings is the per-account switchboard: on/off, model,
// topics, threshold, cap. Keyed on (source, account) so a second channel
// needs no column on its own account entity. Off by default: nothing runs
// and nothing is billed until an operator turns it on.
type CommentAnalysisSettings struct {
	Source      string `gorm:"primaryKey;size:32"`
	AccountID   string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	Enabled           bool           `gorm:"not null;default:false"`
	Model             string         `gorm:"size:120"`
	Vertical          string         `gorm:"size:16;not null;default:'services'"`
	Topics            datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	SeverityThreshold int            `gorm:"not null;default:60"`
	DailyCap          int            `gorm:"not null;default:20000"`
	Instructions      string         `gorm:"type:text"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (CommentAnalysisSettings) TableName() string { return "comment_analysis_settings" }

// CommentAnalysisAuthor is the projection behind "who commented bad things":
// one row per (source, account, author), rebuilt hourly from the analyses.
// moderation_state is operator-set and must survive every rebuild.
type CommentAnalysisAuthor struct {
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
	TopTopics       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	DerivedStance   string         `gorm:"size:16;not null;default:'neutral'"`
	IsFlagged       bool           `gorm:"not null;default:false"`
	ModerationState string         `gorm:"size:16;not null;default:'none'"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (CommentAnalysisAuthor) TableName() string { return "comment_analysis_authors" }

func (a *CommentAnalysisAuthor) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// CommentAnalysisRollup is one (scope, scope_id, day) snapshot, so a 90-day
// trend is an index scan over hundreds of rows instead of COUNT(*) FILTER
// over millions. Historical rows never change when a comment is deleted.
type CommentAnalysisRollup struct {
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

func (CommentAnalysisRollup) TableName() string { return "comment_analysis_rollups" }

// CommentAnalysisBatch is the receipt for one model call: what was sent,
// what came back, what it cost. It is what lets the dashboard say "12.480
// comentários analisados · R$ 37,44 este mês".
type CommentAnalysisBatch struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_batch_ws,priority:1"`
	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null"`
	ContainerID string `gorm:"size:64;not null"`

	Model            string `gorm:"size:120"`
	ItemCount        int    `gorm:"not null;default:0"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	PriceMicros      int64  `gorm:"not null;default:0"`
	Outcome          string `gorm:"size:16;not null"`
	RequestID        string `gorm:"size:128"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz;index:idx_ca_batch_ws,priority:2"`
}

func (CommentAnalysisBatch) TableName() string { return "comment_analysis_batches" }

// CommentAnalysisBackfill is one operator-initiated, resumable pass over
// historical comments. It enqueues; it never classifies.
type CommentAnalysisBackfill struct {
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

func (CommentAnalysisBackfill) TableName() string { return "comment_analysis_backfills" }

func (b *CommentAnalysisBackfill) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

// CommentAnalysisContainerSettings is one post's override of its account's
// settings. Every column is nullable: NULL means "inherit". Kept as its own
// table rather than nullable columns on the account row, so the account tier
// keeps its NOT NULL defaults and a post row stores only what it changes.
type CommentAnalysisContainerSettings struct {
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

func (CommentAnalysisContainerSettings) TableName() string {
	return "comment_analysis_container_settings"
}
