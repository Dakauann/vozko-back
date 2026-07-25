package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SplitType string

const (
	SplitTypeSupplier SplitType = "supplier"
	SplitTypeReseller SplitType = "reseller"
	SplitTypeOwner    SplitType = "owner"
)

type PaymentSplit struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	Name        string         `gorm:"not null;size:255"`
	Type        SplitType      `gorm:"not null;size:32"`
	Provider    string         `gorm:"not null;size:50"`
	WalletID    string         `gorm:"not null;size:255"`
	Percentage  float64        `gorm:"type:decimal(10,2);default:0"`
	FixedAmount float64        `gorm:"type:decimal(10,2);default:0"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (PaymentSplit) TableName() string {
	return "payment_splits"
}

func (ps *PaymentSplit) BeforeCreate(tx *gorm.DB) error {
	if ps.ID == "" {
		ps.ID = uuid.New().String()
	}
	return nil
}
