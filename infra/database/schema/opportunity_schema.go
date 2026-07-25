package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Opportunity is the GORM row model for a sales deal ("oportunidade"). It is kept
// separate from the domain opportunity.Opportunity entity and hand-mapped in the
// repository. The typed custom-field values are stored as jsonb via
// datatypes.JSON.
//
// MONEY GUARDRAIL: ValueCents is the CUSTOMER's own sales amount in the minor
// unit of Currency (default BRL). It is NOT Vozko platform money and never flows
// through the USD-micros wallet / saldo / ledger / Asaas / FX.
//
// Added to AutoMigrate in migrate.go (see WIRING notes).
type Opportunity struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_opportunity_ws_pipe_stage,priority:1;index:idx_opportunity_ws_owner,priority:1"`
	LeadID      string `gorm:"type:uuid;default:null"`
	PipelineID  string `gorm:"type:uuid;not null;index:idx_opportunity_ws_pipe_stage,priority:2"`
	StageID     string `gorm:"type:uuid;not null;index:idx_opportunity_ws_pipe_stage,priority:3"`
	OwnerID     string `gorm:"type:uuid;default:null;index:idx_opportunity_ws_owner,priority:2"`
	CarteiraID  string `gorm:"type:uuid;default:null"`

	Title      string `gorm:"size:255"`
	ValueCents int64  `gorm:"type:bigint;not null;default:0"`
	Currency   string `gorm:"type:varchar(3);not null;default:BRL"`
	Status     string `gorm:"type:varchar(10);not null;default:open"`

	// Free-text loss reason for now (e.g. "preço", "concorrente"). A curated
	// lost-reason catalog can replace this later; kept as text so the required
	// won/lost move works without a catalog and Phase-4 reporting can group by it.
	LostReasonID string     `gorm:"type:text;default:null"`
	Source       string     `gorm:"size:120;default:null"`
	CloseDate    *time.Time `gorm:"default:null"`

	CustomFields datatypes.JSON `gorm:"type:jsonb"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (Opportunity) TableName() string {
	return "opportunities"
}

func (o *Opportunity) BeforeCreate(tx *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	return nil
}

// OpportunityConversation links a conversation (entry) to an opportunity so the
// deal timeline can aggregate its WhatsApp/voice history. It carries exactly the
// domain opportunity.ConversationLink shape (opportunity_id, entry_id,
// entry_type); workspace scoping is enforced by joining to opportunities. It is
// indexed in both directions and unique per (opportunity, entry).
//
// Added to AutoMigrate in migrate.go (see WIRING notes).
type OpportunityConversation struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	OpportunityID string    `gorm:"type:uuid;not null;index:idx_opp_conv_opportunity;uniqueIndex:idx_opp_conv_unique,priority:1"`
	EntryID       string    `gorm:"type:uuid;not null;index:idx_opp_conv_entry,priority:1;uniqueIndex:idx_opp_conv_unique,priority:2"`
	EntryType     string    `gorm:"type:varchar(20);not null;index:idx_opp_conv_entry,priority:2;uniqueIndex:idx_opp_conv_unique,priority:3"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

func (OpportunityConversation) TableName() string {
	return "opportunity_conversations"
}

func (l *OpportunityConversation) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}
