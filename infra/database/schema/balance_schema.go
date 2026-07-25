package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Balance struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"uniqueIndex;not null;type:uuid"`
	Amount      int64     `gorm:"not null;default:0"`
	Currency    string    `gorm:"not null;size:3;default:'USD'"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Workspace    Workspace            `gorm:"foreignKey:WorkspaceID;references:ID"`
	Transactions []BalanceTransaction `gorm:"foreignKey:BalanceID;references:ID"`
}

func (Balance) TableName() string {
	return "balances"
}

func (b *Balance) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

type BalanceTransaction struct {
	ID                 string    `gorm:"primaryKey;type:uuid"`
	BalanceID          string    `gorm:"index;not null;type:uuid"`
	WorkspaceID        string    `gorm:"index;not null;type:uuid;index:idx_bt_ws_created_svc,priority:1,where:type = 'debit' AND is_refund = false"`
	Type               string    `gorm:"not null;size:20"`
	Amount             int64     `gorm:"not null"`
	ResourceType       string    `gorm:"not null;size:30;default:'money'"`
	BalanceBefore      int64     `gorm:"not null"`
	BalanceAfter       int64     `gorm:"not null"`
	ServiceType        string    `gorm:"not null;size:50;index:idx_bt_ws_created_svc,priority:3;index:idx_bt_created_svc,priority:2"`
	ReferenceID        *string   `gorm:"type:varchar(255);index"`
	Description        string    `gorm:"size:500"`
	CostMicros         int64     `gorm:"not null;default:0"`
	ProfitMicros       int64     `gorm:"not null;default:0"`
	ExchangeRateMicros int64     `gorm:"not null;default:0"`
	IsRefund           bool      `gorm:"not null;default:false"`
	CreatedAt          time.Time `gorm:"autoCreateTime;index;index:idx_bt_ws_created_svc,priority:2;index:idx_bt_created_svc,priority:1,where:type = 'debit' AND is_refund = false"`

	Balance Balance `gorm:"foreignKey:BalanceID;references:ID"`
}

func (BalanceTransaction) TableName() string {
	return "balance_transactions"
}

func (t *BalanceTransaction) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
