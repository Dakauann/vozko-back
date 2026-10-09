package schema

import (
	"time"

	"gorm.io/datatypes"
)

type LeadImport struct {
	ID               string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string         `gorm:"type:uuid;not null;index:idx_lead_imports_workspace_created,priority:1"`
	RequestedBy      string         `gorm:"type:uuid;not null"`
	RequestedByAdmin bool           `gorm:"not null;default:false"`
	Status           string         `gorm:"type:varchar(16);not null;index:idx_lead_imports_status_heartbeat,priority:1"`
	Stage            OptionalText   `gorm:"type:varchar(16)"`
	MediaID          OptionalText   `gorm:"type:uuid"`
	FileName         string         `gorm:"type:varchar(255);not null"`
	FileOwned        bool           `gorm:"not null;default:false"`
	SizeBytes        int64          `gorm:"not null;default:0"`
	TotalRows        int            `gorm:"not null;default:0"`
	Preview          datatypes.JSON `gorm:"type:jsonb;not null"`
	Settings         datatypes.JSON `gorm:"type:jsonb"`
	Grants           datatypes.JSON `gorm:"type:jsonb"`
	Fingerprint      OptionalText   `gorm:"type:varchar(64)"`
	DryRun           datatypes.JSON `gorm:"type:jsonb"`
	Result           datatypes.JSON `gorm:"type:jsonb"`
	Seed             datatypes.JSON `gorm:"type:jsonb"`
	Processed        int            `gorm:"not null;default:0"`
	FailureCode      OptionalText   `gorm:"type:varchar(32)"`
	Attempts         int            `gorm:"not null;default:0"`
	ClaimToken       OptionalText   `gorm:"type:varchar(64)"`
	HeartbeatAt      *time.Time     `gorm:"type:timestamptz;index:idx_lead_imports_status_heartbeat,priority:2"`
	StartedAt        *time.Time     `gorm:"type:timestamptz"`
	FinishedAt       *time.Time     `gorm:"type:timestamptz"`
	ExpiresAt        time.Time      `gorm:"type:timestamptz;not null;index:idx_lead_imports_expires_at"`
	CreatedAt        time.Time      `gorm:"type:timestamptz;not null;index:idx_lead_imports_workspace_created,priority:2"`
	UpdatedAt        time.Time      `gorm:"type:timestamptz;not null"`
}

func (LeadImport) TableName() string {
	return "lead_imports"
}

type LeadImportIssue struct {
	Seq      int64        `gorm:"primaryKey;autoIncrement;index:idx_lead_import_issues_page,priority:2"`
	ImportID string       `gorm:"type:uuid;not null;index:idx_lead_import_issues_page,priority:1"`
	Import   *LeadImport  `gorm:"foreignKey:ImportID;references:ID;constraint:OnDelete:CASCADE"`
	Line     int          `gorm:"not null"`
	Reason   string       `gorm:"type:varchar(48);not null"`
	Field    OptionalText `gorm:"type:text"`
	Rejected bool         `gorm:"not null;default:false"`
}

func (LeadImportIssue) TableName() string {
	return "lead_import_issues"
}

type LeadImportLink struct {
	ID             string      `gorm:"primaryKey;type:uuid;index:idx_lead_import_links_page,priority:3"`
	ImportID       string      `gorm:"type:uuid;not null;index:idx_lead_import_links_page,priority:1"`
	Import         *LeadImport `gorm:"foreignKey:ImportID;references:ID;constraint:OnDelete:CASCADE"`
	WorkspaceID    string      `gorm:"type:uuid;not null"`
	Line           int         `gorm:"not null;index:idx_lead_import_links_page,priority:2"`
	LeadID         string      `gorm:"type:uuid;not null"`
	RelativeNumber string      `gorm:"type:varchar(20);not null"`
	Kind           string      `gorm:"type:varchar(24);not null"`
}

func (LeadImportLink) TableName() string {
	return "lead_import_links"
}
