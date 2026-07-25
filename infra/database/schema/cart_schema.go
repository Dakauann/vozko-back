package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Cart struct {
	ID        string         `gorm:"primaryKey;type:uuid"`
	UserID    string         `gorm:"not null;type:uuid;uniqueIndex"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	Items     []CartItem     `gorm:"foreignKey:CartID;constraint:OnDelete:CASCADE"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (Cart) TableName() string {
	return "carts"
}

func (c *Cart) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type CartItem struct {
	ID         string           `gorm:"primaryKey;type:uuid"`
	CartID     string           `gorm:"not null;type:uuid;index"`
	ProductID  string           `gorm:"not null;type:uuid;index"`
	VariantID  string           `gorm:"not null;type:uuid;index"`
	Quantity   int              `gorm:"not null;default:1"`
	UnitPrice  float64          `gorm:"not null"`
	TotalPrice float64          `gorm:"not null"`
	Options    []CartItemOption `gorm:"foreignKey:CartItemID;constraint:OnDelete:CASCADE"`
	CreatedAt  time.Time        `gorm:"autoCreateTime"`
	UpdatedAt  time.Time        `gorm:"autoUpdateTime"`
	Cart       Cart             `gorm:"foreignKey:CartID"`
	DeletedAt  gorm.DeletedAt   `gorm:"index"`
}

func (CartItem) TableName() string {
	return "cart_items"
}

func (ci *CartItem) BeforeCreate(tx *gorm.DB) error {
	if ci.ID == "" {
		ci.ID = uuid.New().String()
	}
	return nil
}

type CartItemOption struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	CartItemID  string         `gorm:"not null;type:uuid;index"`
	OptionType  string         `gorm:"not null;type:varchar(100)"`
	OptionValue string         `gorm:"not null;type:varchar(255)"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (CartItemOption) TableName() string {
	return "cart_item_options"
}

func (cio *CartItemOption) BeforeCreate(tx *gorm.DB) error {
	if cio.ID == "" {
		cio.ID = uuid.New().String()
	}
	return nil
}
