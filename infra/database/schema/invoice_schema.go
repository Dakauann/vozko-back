package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Invoice struct {
	ID               string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string  `gorm:"not null;type:uuid;index"`
	UserID           string  `gorm:"not null;type:uuid;index"`
	Purpose          string  `gorm:"not null;size:30;default:'TOP_UP';index"`
	PlanDefinitionID *string `gorm:"type:uuid;index"`
	AmountBRL        float64 `gorm:"not null;type:decimal(12,2)"`
	AmountUSD        int64   `gorm:"not null;default:0"`
	CreditableUSD    int64   `gorm:"not null;default:0"`
	ExchangeRate     float64 `gorm:"not null;type:decimal(12,6)"`
	Status           string  `gorm:"not null;size:20;default:'PENDING';index"`
	BillingType      string  `gorm:"not null;size:20"`
	BillingCycle     string  `gorm:"size:20;default:'monthly'"`
	ExternalID       string  `gorm:"not null;size:255;uniqueIndex"`
	IdempotencyKey   string  `gorm:"size:160;not null;default:''"`
	PixQrCode        *string `gorm:"type:text"`
	PixCopy          *string `gorm:"type:text"`
	BankSlipUrl      *string `gorm:"type:text"`
	InvoiceUrl       *string `gorm:"type:text"`
	PaidAt           *int64
	Description      string     `gorm:"size:500"`
	DueDate          *time.Time `gorm:"index"`
	// LineItemsJSON stores the customer-facing breakdown (price only) as JSON. Empty for single-amount
	// invoices. Stored as JSON text so no extra driver dependency is needed.
	LineItemsJSON string    `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt     time.Time `gorm:"autoCreateTime;index"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

func (Invoice) TableName() string { return "invoices" }

func (i *Invoice) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}
