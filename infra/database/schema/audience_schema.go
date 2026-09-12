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
// Five tables. audience_analyses is the QUEUE as well as the result store:
// a row is inserted pending at ingest and the database, not Redis, says what
// is waiting. The partial indexes that make the queue cheap (pending by
// container, stale in_flight, the unique (source, subject_id) that
// makes webhook redelivery free) are declared in indexes.go because GORM
// tags cannot express a WHERE clause.

// AudienceAnalysis is one classified comment. Comment TEXT is never stored
// here (only a 200-rune excerpt): it lives in the channel's own table and
// inherits that table's retention and PII posture.
type AudienceAnalysis struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_workspace"`

	// SubjectKind is 'comment' or 'conversation'. The default matters: every
	// row written before conversations existed is a comment, and the default
	// backfills them on migration without a data pass.
	SubjectKind string `gorm:"size:16;not null;default:'comment'"`
	Revision    string `gorm:"size:64;not null;default:''"`
	// Bounded conversation snapshot, purged with its analysis. Never populated
	// for public comments and never exposed by the list DTO.
	Transcript string `gorm:"type:text;not null;default:''"`

	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_ca_account"`
	ContainerID string `gorm:"size:64;not null"`
	// SubjectID identifies the subject on its channel: a comment id, or a
	// conversation's entry id.
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

	// Conversation labels. Null/zero for every comment row. They are columns
	// rather than a jsonb blob because the audience view filters, groups and
	// averages on all of them, and a blob would push that work into the
	// application.
	Interest        string `gorm:"size:24"`
	ProductInterest string `gorm:"size:160"`
	// ProductInterestKey is the countable form of ProductInterest: the same
	// subject written three ways collides here so it can be grouped. Its index
	// is declared with the other composite ones in database/indexes.go, because
	// the query it serves is always scoped to a workspace first.
	ProductInterestKey string `gorm:"size:64;not null;default:''"`
	Disposition        string `gorm:"size:24"`
	Qualification      string `gorm:"size:16"`
	NextAction         string `gorm:"size:24"`
	// Summary is the model's prose and the only unredacted customer content in
	// this table, which is why it is subject to the same retention as the rest
	// of the row rather than living somewhere unswept.
	Summary string `gorm:"type:text"`
	// AttendanceQuality is 0-100, computed from the ordinal rubric, never
	// model-set.
	AttendanceQuality int `gorm:"not null;default:0"`
	MessageCount      int `gorm:"not null;default:0"`

	RequiresAction bool   `gorm:"not null;default:false"`
	Excerpt        string `gorm:"size:800"`
	Truncated      bool   `gorm:"not null;default:false"`

	BatchID    *string    `gorm:"type:uuid"`
	Model      string     `gorm:"size:120"`
	AnalyzedAt *time.Time `gorm:"type:timestamptz"`

	// OccurredAt is the channel's timestamp; rollups bucket by it.
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

// AudienceSettings is the per-account switchboard: on/off, model,
// topics, threshold, cap. Keyed on (source, account) so a second channel
// needs no column on its own account entity. Off by default: nothing runs
// and nothing is billed until an operator turns it on.
type AudienceSettings struct {
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
	// Replying (§6). Off by default at the column level too, so an account row
	// written before this existed reads back as "does not reply" rather than
	// inheriting whatever the zero value of a future default happens to be.
	ReplyMode            string `gorm:"size:16;not null;default:'off'"`
	ReplyMaxAutoSeverity int    `gorm:"not null;default:30"`

	CreatedAt time.Time `gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz"`
}

func (AudienceSettings) TableName() string { return "audience_settings" }

// AudienceAuthor is the projection behind "who commented bad things":
// one row per (source, account, author), rebuilt hourly from the analyses.
// moderation_state is operator-set and must survive every rebuild.
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
	// Reputation is the signed ledger, denormalised for the same reason the
	// counts above it are: the ranking sorts on it, and parsing jsonb per row
	// cannot use an index. Derived, never accumulated — AuthorStats.Derive
	// recomputes it from the counters on every rollup.
	Reputation      int            `gorm:"not null;default:0;index"`
	TopTopics       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	DerivedStance   string         `gorm:"size:16;not null;default:'neutral'"`
	IsFlagged       bool           `gorm:"not null;default:false"`
	ModerationState string         `gorm:"size:16;not null;default:'none'"`
	// The §5 role inference. Columns rather than jsonb because a customer will
	// want to filter by role, and because these have to be preserved across the
	// rollup's rebuild the way moderation_state is.
	Role           string `gorm:"size:24;not null;default:'unknown'"`
	RoleConfidence string `gorm:"size:8;not null;default:'none'"`
	RoleComments   int    `gorm:"not null;default:0"`
	RoleRationale  string `gorm:"size:220"`

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

// AudienceRollup is one (scope, scope_id, day) snapshot, so a 90-day
// trend is an index scan over hundreds of rows instead of COUNT(*) FILTER
// over millions. Historical rows never change when a comment is deleted.
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

// AudienceBatch is the receipt for one model call: what was sent,
// what came back, what it cost. It is what lets the dashboard say "12.480
// comentários analisados · R$ 37,44 este mês".
type AudienceBatch struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_batch_ws,priority:1"`
	Source      string `gorm:"size:32;not null"`
	AccountID   string `gorm:"type:uuid;not null"`
	ContainerID string `gorm:"size:64;not null"`

	// Kind is which pass bought these tokens. Defaulted rather than nullable so
	// every row written before the author pass existed reads as the comment
	// pass, which is what it is.
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

// AudienceBackfill is one operator-initiated, resumable pass over
// historical comments. It enqueues; it never classifies.
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

// AudienceContainerSettings is one post's override of its account's
// settings. Every column is nullable: NULL means "inherit". Kept as its own
// table rather than nullable columns on the account row, so the account tier
// keeps its NOT NULL defaults and a post row stores only what it changes.
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

// AudienceAlertRule is one configured watch: "quando passar de X, manda
// um WhatsApp para Y".
//
// The firing history lives on the rule rather than in a second table because
// it is not an audit log, it is the state the cooldown and the daily cap are
// checked against, and both are checked inside the same conditional UPDATE
// that claims the firing. A separate table would put the guard and the counter
// in two statements.
type AudienceAlertRule struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_ca_alert_ws"`
	Source      string `gorm:"size:32;not null;index:idx_ca_alert_armed,priority:1"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_ca_alert_armed,priority:2"`

	Name string `gorm:"size:80;not null"`
	// Enabled is part of the armed-rules index key: the engine asks for one
	// account's live rules after every batch, and that read must not scan.
	Enabled bool `gorm:"not null;default:false;index:idx_ca_alert_armed,priority:3"`
	// The optional ids are POINTERS because they are uuid columns and an empty
	// string is not a uuid: stored as a value they would be rejected outright
	// on every rule that does not use them.
	CreatedByUserID *string `gorm:"type:uuid"`

	Metric        string `gorm:"size:32;not null"`
	Threshold     int    `gorm:"not null;default:0"`
	WindowMinutes int    `gorm:"not null;default:0"`
	// MinMessages is the conversation-length floor. Zero is "no floor", which
	// is every rule written before this column existed.
	MinMessages int `gorm:"not null;default:0"`

	Channel         string  `gorm:"size:16;not null"`
	Recipient       string  `gorm:"size:32;not null"`
	BusinessPhoneID *string `gorm:"type:uuid"`
	TemplateID      *string `gorm:"type:uuid"`
	InstanceID      *string `gorm:"type:uuid"`

	// Brief adds the model's reading to the message. Off by default: it is one
	// AI call per firing.
	Brief bool `gorm:"not null;default:false"`

	CooldownMinutes int `gorm:"not null;default:60"`
	MaxPerDay       int `gorm:"not null;default:6"`

	LastFiredAt *time.Time `gorm:"type:timestamptz"`
	// FiredDay is a plain date string rather than a date column: it is only
	// ever compared for equality against today, and keeping it textual lets the
	// claim's CASE expression stay readable.
	FiredToday int    `gorm:"not null;default:0"`
	FiredDay   string `gorm:"size:10;not null;default:''"`
	LastError  string `gorm:"size:300;not null;default:''"`

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
