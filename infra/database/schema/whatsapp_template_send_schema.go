package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WhatsAppTemplateSend is one paid template send, and the row that makes
// "charged exactly once" a property of the database.
//
// The balance ledger cannot provide that on its own: its reference_id sits on a
// NON-unique index, so two debits carrying the same reference are simply two
// debits. This table supplies the missing uniqueness, and the send is what the
// single winner of that insert is allowed to do afterwards.
//
// IdempotencyKey is unique PER WORKSPACE, enforced by a partial unique index in
// indexes.go — GORM tags cannot express the `WHERE deleted_at IS NULL` predicate,
// and scoping to the workspace matters because the key is chosen by the caller:
// global uniqueness would let one tenant deny another's send by guessing a key.
type WhatsAppTemplateSend struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_wats_ws"`
	// UserID is the operator who spent the money. The ledger has no column for a
	// person, so this is the only place a charge is attributable to a human.
	UserID         string `gorm:"type:uuid;index"`
	IdempotencyKey string `gorm:"size:191;not null"`

	BusinessPhoneID string `gorm:"type:uuid;not null;index"`
	TemplateID      string `gorm:"type:uuid;not null"`
	TemplateName    string `gorm:"size:512"`
	Language        string `gorm:"size:20"`
	Category        string `gorm:"size:20"`
	ToNumber        string `gorm:"size:32;index"`

	CampaignID *string `gorm:"type:uuid;index"`
	EntryID    *string `gorm:"type:uuid;index"`

	Status string `gorm:"size:20;not null;default:'pending';index"`

	// ChargedMicros records what was actually taken. Refunds re-price at today's
	// prices, so without this a refund can silently disagree with its debit.
	ChargedMicros int64 `gorm:"not null;default:0"`
	// ProviderMessageID is Meta's wamid. Column pinned explicitly because GORM's
	// initialism handling would otherwise derive something else.
	ProviderMessageID string `gorm:"column:provider_message_id;size:191"`
	ResponseStatus    int    `gorm:"default:0"`
	ErrorCode         int    `gorm:"default:0"`
	ErrorMessage      string `gorm:"type:text"`

	ChargedAt  *time.Time
	SentAt     *time.Time
	RefundedAt *time.Time

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;index"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (WhatsAppTemplateSend) TableName() string {
	return "whatsapp_template_sends"
}

func (s *WhatsAppTemplateSend) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
