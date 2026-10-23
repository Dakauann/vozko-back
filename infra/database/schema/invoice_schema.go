package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Invoice struct {
	ID                string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string  `gorm:"not null;type:uuid;index"`
	UserID            string  `gorm:"not null;type:uuid;index"`
	Purpose           string  `gorm:"not null;size:30;default:'TOP_UP';index"`
	PlanDefinitionID  *string `gorm:"type:uuid;index"`
	AmountBRL         float64 `gorm:"not null;type:decimal(12,2)"`
	AmountUSD         int64   `gorm:"not null;default:0"`
	CreditableUSD     int64   `gorm:"not null;default:0"`
	ExchangeRate      float64 `gorm:"not null;type:decimal(12,6)"`
	Status            string  `gorm:"not null;size:20;default:'PENDING';index"`
	BillingType       string  `gorm:"not null;size:20"`
	BillingCycle      string  `gorm:"size:20;default:'monthly'"`
	Provider          string  `gorm:"size:20;not null;default:''"`
	ExternalID        *string `gorm:"size:255"`
	IdempotencyKey    string  `gorm:"size:160;not null;default:''"`
	PixQrCode         *string `gorm:"type:text"`
	PixCopy           *string `gorm:"type:text"`
	BankSlipUrl       *string `gorm:"type:text"`
	InvoiceUrl        *string `gorm:"type:text"`
	ChargeExpiresAt   *time.Time
	RenewalLeaseUntil *time.Time
	PaidAt            *int64
	Description       string     `gorm:"size:500"`
	DueDate           *time.Time `gorm:"index"`
	LineItemsJSON     string     `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt         time.Time  `gorm:"autoCreateTime;index"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime"`
}

func (Invoice) TableName() string { return "invoices" }

func (i *Invoice) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}

type InvoiceSupersededCharge struct {
	ID         string    `gorm:"primaryKey;type:uuid"`
	InvoiceID  string    `gorm:"not null;type:uuid;index"`
	Provider   string    `gorm:"size:20;not null;uniqueIndex:ux_invoice_superseded_charges_provider_external_id,priority:1"`
	ExternalID string    `gorm:"size:255;not null;uniqueIndex:ux_invoice_superseded_charges_provider_external_id,priority:2"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

func (InvoiceSupersededCharge) TableName() string { return "invoice_superseded_charges" }

func (c *InvoiceSupersededCharge) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
