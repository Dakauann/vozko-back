package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Affiliate struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	UserID        string    `gorm:"type:uuid;not null;uniqueIndex:idx_affiliates_user"`
	Code          string    `gorm:"type:varchar(32);not null;uniqueIndex:idx_affiliates_code"`
	BrandName     string    `gorm:"type:varchar(128);not null;default:''"`
	BrandLogoURL  string    `gorm:"type:varchar(512);not null;default:''"`
	AsaasWalletID string    `gorm:"type:varchar(64);not null"`
	CommissionPct float64   `gorm:"type:decimal(5,4);not null;default:0.05"`
	Tier          string    `gorm:"type:varchar(16);not null;default:'affiliate'"`
	Active        bool      `gorm:"not null;default:true"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

func (Affiliate) TableName() string { return "affiliates" }

func (a *Affiliate) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type AffiliateReferral struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	AffiliateID string    `gorm:"type:uuid;not null;index:idx_affiliate_referrals_affiliate"`
	WorkspaceID string    `gorm:"type:uuid;not null;uniqueIndex:idx_affiliate_referrals_workspace"`
	ReferredAt  time.Time `gorm:"autoCreateTime"`
}

func (AffiliateReferral) TableName() string { return "affiliate_referrals" }

func (r *AffiliateReferral) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

type AffiliateEarning struct {
	ID                 string    `gorm:"primaryKey;type:uuid"`
	AffiliateID        string    `gorm:"type:uuid;not null;index:idx_affiliate_earnings_affiliate"`
	InvoiceID          string    `gorm:"type:uuid;not null;uniqueIndex:idx_affiliate_earnings_invoice"`
	WorkspaceID        string    `gorm:"type:uuid;not null;index:idx_affiliate_earnings_workspace"`
	AmountMicros       int64     `gorm:"not null"`
	ExchangeRateMicros int64     `gorm:"not null;default:0"`
	Purpose            string    `gorm:"type:varchar(32);not null"`
	Status             string    `gorm:"type:varchar(16);not null;default:'paid'"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
}

func (AffiliateEarning) TableName() string { return "affiliate_earnings" }

func (e *AffiliateEarning) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
