package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type AdGrant struct {
	ID               string                  `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string                  `gorm:"type:uuid;not null;index;index:idx_ad_grant_identity,priority:1"`
	ConnectedBy      string                  `gorm:"type:uuid;not null"`
	TokenKind        string                  `gorm:"size:16;not null"`
	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	TokenExpiresAt   *time.Time              `gorm:"type:timestamptz"`
	AppScopedUserID  string                  `gorm:"size:64;not null;index:idx_ad_grant_identity,priority:2"`
	ClientBusinessID string                  `gorm:"size:64;index:idx_ad_grant_identity,priority:3"`
	Scopes           string                  `gorm:"type:text"`
	GranularScopes   datatypes.JSON          `gorm:"type:jsonb;default:'{}'"`
	Status           string                  `gorm:"size:16;not null;default:'ACTIVE';index"`
	CheckedAt        *time.Time              `gorm:"type:timestamptz"`
	RevokedAt        *time.Time              `gorm:"type:timestamptz"`
	CreatedAt        time.Time               `gorm:"autoCreateTime"`
	UpdatedAt        time.Time               `gorm:"autoUpdateTime"`
	DeletedAt        gorm.DeletedAt          `gorm:"index"`
}

func (AdGrant) TableName() string { return "ad_grants" }

func (g *AdGrant) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

type AdAccount struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index"`
	GrantID       string         `gorm:"type:uuid;not null;index"`
	MetaAccountID string         `gorm:"size:64;not null;uniqueIndex"`
	Name          string         `gorm:"size:255;not null;default:''"`
	BusinessID    string         `gorm:"size:64;not null;default:''"`
	BusinessName  string         `gorm:"size:255;not null;default:''"`
	Currency      string         `gorm:"type:varchar(3);not null;default:''"`
	Timezone      string         `gorm:"size:64;not null;default:''"`
	MetaStatus    int            `gorm:"not null;default:0"`
	DisableReason int            `gorm:"not null;default:0"`
	HasFunding    bool           `gorm:"not null;default:false"`
	AmountSpent   int64          `gorm:"type:bigint;not null;default:0"`
	SpendCap      int64          `gorm:"type:bigint;not null;default:0"`
	UserTasks     pq.StringArray `gorm:"type:text[];not null;default:'{}'"`
	Connection    string         `gorm:"type:varchar(24);not null;index"`
	LastSyncedAt  *time.Time     `gorm:"type:timestamptz"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
}

func (AdAccount) TableName() string { return "ad_accounts" }

func (a *AdAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type AdObject struct {
	MetaID           string         `gorm:"primaryKey;size:64"`
	WorkspaceID      string         `gorm:"type:uuid;not null;index"`
	AdAccountID      string         `gorm:"type:uuid;not null;index:idx_ad_object_account_level,priority:1"`
	Level            string         `gorm:"type:varchar(16);not null;index:idx_ad_object_account_level,priority:2"`
	CampaignMetaID   string         `gorm:"size:64;not null;default:'';index"`
	AdSetMetaID      string         `gorm:"column:adset_meta_id;size:64;not null;default:'';index"`
	Name             string         `gorm:"type:text;not null;default:''"`
	Status           string         `gorm:"size:32;not null;default:''"`
	EffectiveStatus  string         `gorm:"size:32;not null;default:''"`
	Objective        string         `gorm:"size:64;not null;default:''"`
	SpecialCategory  string         `gorm:"type:varchar(48);not null;default:''"`
	DestinationType  string         `gorm:"size:64;not null;default:''"`
	OptimizationGoal string         `gorm:"size:64;not null;default:''"`
	BidStrategy      string         `gorm:"size:64;not null;default:''"`
	DailyBudget      int64          `gorm:"type:bigint;not null;default:0"`
	LifetimeBudget   int64          `gorm:"type:bigint;not null;default:0"`
	BudgetRemaining  int64          `gorm:"type:bigint;not null;default:0"`
	StartTime        *time.Time     `gorm:"type:timestamptz"`
	EndTime          *time.Time     `gorm:"type:timestamptz"`
	Creative         datatypes.JSON `gorm:"type:jsonb"`
	ReviewFeedback   datatypes.JSON `gorm:"type:jsonb"`
	Issues           datatypes.JSON `gorm:"type:jsonb"`
	BudgetChanges    datatypes.JSON `gorm:"type:jsonb"`
	Removed          bool           `gorm:"not null;default:false"`
	CreatedTime      *time.Time     `gorm:"type:timestamptz"`
	UpdatedTime      *time.Time     `gorm:"type:timestamptz"`
	SyncedAt         time.Time      `gorm:"type:timestamptz;not null"`
}

func (AdObject) TableName() string { return "ad_objects" }

type AdInsightDaily struct {
	AdMetaID       string         `gorm:"primaryKey;size:64"`
	Day            time.Time      `gorm:"primaryKey;type:date"`
	AdAccountID    string         `gorm:"type:uuid;not null;index:idx_ad_insight_account_day,priority:1"`
	CampaignMetaID string         `gorm:"size:64;not null;default:''"`
	AdSetMetaID    string         `gorm:"column:adset_meta_id;size:64;not null;default:''"`
	Currency       string         `gorm:"type:varchar(3);not null;default:''"`
	SpendMicros    int64          `gorm:"type:bigint;not null;default:0"`
	Impressions    int64          `gorm:"type:bigint;not null;default:0"`
	Clicks         int64          `gorm:"type:bigint;not null;default:0"`
	LinkClicks     int64          `gorm:"type:bigint;not null;default:0"`
	Actions        datatypes.JSON `gorm:"type:jsonb"`
	FetchedAt      time.Time      `gorm:"type:timestamptz;not null"`
}

func (AdInsightDaily) TableName() string { return "ad_insights_daily" }

type AdPublishJob struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index"`
	AdAccountID  string         `gorm:"type:uuid;not null;index"`
	CreatedBy    string         `gorm:"type:uuid;not null"`
	Actor        string         `gorm:"size:16;not null"`
	Draft        datatypes.JSON `gorm:"type:jsonb;not null"`
	Status       string         `gorm:"size:16;not null;index:idx_ad_publish_job_status_updated,priority:1"`
	Progress     datatypes.JSON `gorm:"type:jsonb"`
	Fee          string         `gorm:"type:varchar(16);not null;default:''"`
	FeeMicros    int64          `gorm:"type:bigint;not null;default:0"`
	ErrorCode    string         `gorm:"size:64;not null;default:''"`
	ErrorMessage string         `gorm:"type:text;not null;default:''"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime;index:idx_ad_publish_job_status_updated,priority:2"`
}

func (AdPublishJob) TableName() string { return "ad_publish_jobs" }

func (j *AdPublishJob) BeforeCreate(tx *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	return nil
}

type AdSavedAudience struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index"`
	Name        string         `gorm:"size:400;not null"`
	Targeting   datatypes.JSON `gorm:"type:jsonb;not null"`
	Placements  datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedBy   string         `gorm:"type:uuid;not null"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
}

func (AdSavedAudience) TableName() string { return "ad_saved_audiences" }

func (s *AdSavedAudience) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type AdLeadForm struct {
	MetaID       string     `gorm:"primaryKey;size:64"`
	WorkspaceID  string     `gorm:"type:uuid;not null;index"`
	AdAccountID  string     `gorm:"type:uuid;not null;index"`
	PageID       string     `gorm:"size:64;not null"`
	Name         string     `gorm:"type:text;not null;default:''"`
	LastPolledAt *time.Time `gorm:"type:timestamptz;index"`
}

func (AdLeadForm) TableName() string { return "ad_lead_forms" }

type AdFormLead struct {
	MetaID      string         `gorm:"primaryKey;size:64"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_ad_form_lead_ws_created,priority:1"`
	FormMetaID  string         `gorm:"size:64;not null;index"`
	AdMetaID    string         `gorm:"size:64;not null;default:''"`
	PageID      string         `gorm:"size:64;not null;default:''"`
	Answers     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	LeadID      *string        `gorm:"type:uuid;index"`
	CreatedTime time.Time      `gorm:"type:timestamptz;not null;index:idx_ad_form_lead_ws_created,priority:2"`
}

func (AdFormLead) TableName() string { return "ad_form_leads" }

type AdConversionSettings struct {
	WorkspaceID   string    `gorm:"primaryKey;type:uuid"`
	AdAccountID   string    `gorm:"type:uuid;not null"`
	DatasetID     string    `gorm:"size:64;not null;default:''"`
	PixelID       string    `gorm:"size:64;not null;default:''"`
	SendLeads     bool      `gorm:"not null;default:false"`
	SendPurchases bool      `gorm:"not null;default:false"`
	Enabled       bool      `gorm:"not null;default:false;index"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

func (AdConversionSettings) TableName() string { return "ad_conversion_settings" }

type AdConversionRecord struct {
	OpportunityID string     `gorm:"primaryKey;type:uuid"`
	EventName     string     `gorm:"primaryKey;size:32"`
	WorkspaceID   string     `gorm:"type:uuid;not null;index:idx_ad_conversion_record_ws_updated,priority:1"`
	Status        string     `gorm:"size:16;not null"`
	Reason        string     `gorm:"type:text;not null;default:''"`
	Attempts      int        `gorm:"not null;default:0"`
	SentAt        *time.Time `gorm:"type:timestamptz"`
	UpdatedAt     time.Time  `gorm:"type:timestamptz;not null;index:idx_ad_conversion_record_ws_updated,priority:2"`
}

func (AdConversionRecord) TableName() string { return "ad_conversion_records" }
