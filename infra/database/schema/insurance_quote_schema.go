package schema

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type InsuranceQuotation struct {
	ID         string           `gorm:"primaryKey;type:uuid"`
	UserID     string           `gorm:"not null;type:uuid;index"`
	PolicyType string           `gorm:"not null;type:varchar(64);index"`
	CreatedAt  time.Time        `gorm:"autoCreateTime"`
	UpdatedAt  time.Time        `gorm:"autoUpdateTime"`
	Quotes     []InsuranceQuote `gorm:"foreignKey:QuotationID;references:ID"`
}

func (InsuranceQuotation) TableName() string {
	return "insurance_quotations"
}

func (q *InsuranceQuotation) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(q.ID) == "" {
		q.ID = uuid.NewString()
	}
	return nil
}

type InsuranceQuote struct {
	ID          string            `gorm:"primaryKey;type:uuid"`
	QuotationID string            `gorm:"not null;type:uuid;index"`
	ExternalID  string            `gorm:"type:varchar(255);index"`
	UserID      string            `gorm:"not null;type:uuid;index"`
	PolicyType  string            `gorm:"not null;type:varchar(64);index"`
	Provider    string            `gorm:"not null;type:varchar(64);index"`
	CoverageAmt float64           `gorm:"not null"`
	Premium     float64           `gorm:"not null"`
	Currency    string            `gorm:"not null;type:varchar(16)"`
	ValidUntil  *time.Time        `gorm:"index"`
	Metadata    datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt   time.Time         `gorm:"autoCreateTime"`
	UpdatedAt   time.Time         `gorm:"autoUpdateTime"`
}

func (InsuranceQuote) TableName() string {
	return "insurance_quotes"
}

func (q *InsuranceQuote) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(q.ID) == "" {
		q.ID = uuid.NewString()
	}
	if q.Metadata == nil {
		q.Metadata = datatypes.JSONMap{}
	}
	return nil
}

func (q *InsuranceQuote) BeforeSave(tx *gorm.DB) error {
	if q.Metadata == nil {
		q.Metadata = datatypes.JSONMap{}
	}
	return nil
}
