package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type GoogleSheet struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null"`
	AccountID     string         `gorm:"type:uuid;not null;index:idx_google_sheet_account"`
	SpreadsheetID string         `gorm:"size:128;not null"`
	TabID         int64          `gorm:"not null"`
	TabName       string         `gorm:"size:100;not null"`
	Title         string         `gorm:"size:200;not null"`
	Description   string         `gorm:"type:text;not null;default:''"`
	TimeZone      string         `gorm:"size:64;not null"`
	Locale        string         `gorm:"size:16;not null"`
	Columns       datatypes.JSON `gorm:"type:jsonb;not null"`
	Status        string         `gorm:"size:24;not null;default:'active'"`
	Reason        string         `gorm:"size:32;not null;default:''"`
	ReasonDetail  string         `gorm:"size:500;not null;default:''"`
	Version       int            `gorm:"not null;default:1"`
	CreatedBy     *string        `gorm:"type:uuid"`
	LastWrittenAt *time.Time     `gorm:"type:timestamptz"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (GoogleSheet) TableName() string { return "google_sheets" }

func (s *GoogleSheet) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type GoogleSheetRow struct {
	ID             string                  `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string                  `gorm:"type:uuid;not null"`
	SheetID        string                  `gorm:"type:uuid;not null"`
	IdempotencyKey string                  `gorm:"size:200;not null"`
	Cells          piigorm.EncryptedString `gorm:"type:bytea;not null" json:"-"`
	SheetVersion   int                     `gorm:"not null"`
	OriginKind     string                  `gorm:"size:16;not null"`
	AgentID        string                  `gorm:"size:64;not null;default:''"`
	WorkflowID     string                  `gorm:"size:64;not null;default:''"`
	RunID          string                  `gorm:"size:64;not null;default:''"`
	ActorID        string                  `gorm:"size:120;not null;default:''"`
	LeadID         *string                 `gorm:"size:64"`
	EntryID        string                  `gorm:"size:64;not null;default:''"`
	EntryType      string                  `gorm:"size:32;not null;default:''"`
	Status         string                  `gorm:"size:16;not null;default:'pending'"`
	Attempts       int                     `gorm:"not null;default:0"`
	NextAttemptAt  time.Time               `gorm:"type:timestamptz;not null"`
	BatchID        *string                 `gorm:"type:uuid"`
	ClaimedAt      *time.Time              `gorm:"type:timestamptz"`
	RowNumber      *int
	ErrorCode      string     `gorm:"size:64;not null;default:''"`
	ErrorDetail    string     `gorm:"size:500;not null;default:''"`
	CreatedAt      time.Time  `gorm:"type:timestamptz;not null"`
	WrittenAt      *time.Time `gorm:"type:timestamptz"`
}

func (GoogleSheetRow) TableName() string { return "google_sheet_rows" }

func (r *GoogleSheetRow) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

func GoogleSheetModels() []any {
	return []any{&GoogleSheet{}, &GoogleSheetRow{}}
}
