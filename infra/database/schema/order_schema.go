package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

const OrderCustomerDocumentBlindScope = "orders.customer_document"

type Order struct {
	ID                    string                  `gorm:"primaryKey;type:uuid"`
	UserID                string                  `gorm:"not null;type:uuid;index"`
	ShippingAddressID     string                  `gorm:"not null;type:uuid;index"`
	CustomerName          string                  `gorm:"not null;type:varchar(200)"`
	CustomerDocument      piigorm.EncryptedString `gorm:"column:customer_document;type:bytea"`
	CustomerDocumentBlind piigorm.BlindIndex      `gorm:"column:customer_document_blind;type:bytea;index:idx_orders_customer_document_blind"`
	TotalAmount           float64                 `gorm:"not null"`
	Status                string                  `gorm:"not null;type:varchar(20);default:'pending'"`
	ExpiresAt             *time.Time              `gorm:"index"`
	PurchasedCartItemIDs  pq.StringArray          `gorm:"type:text[]"`
	CreatedAt             time.Time               `gorm:"autoCreateTime"`
	UpdatedAt             time.Time               `gorm:"autoUpdateTime"`
	DeletedAt             gorm.DeletedAt          `gorm:"index"`
	Items                 []OrderItem             `gorm:"foreignKey:OrderID;constraint:OnDelete:CASCADE"`
	Payment               *Payment                `gorm:"foreignKey:OrderID;constraint:OnDelete:CASCADE"`
	Ticket                *Ticket                 `gorm:"foreignKey:OrderID;references:ID;constraint:OnDelete:CASCADE"`
}

func (Order) TableName() string {
	return "orders"
}

func (o *Order) BeforeCreate(tx *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	return nil
}

type OrderItem struct {
	ID         string            `gorm:"primaryKey;type:uuid"`
	OrderID    string            `gorm:"not null;type:uuid;index"`
	ProductID  string            `gorm:"not null;type:uuid;index"`
	VariantID  string            `gorm:"not null;type:uuid;index"`
	SKU        string            `gorm:"not null;type:varchar(100)"`
	Quantity   int               `gorm:"not null"`
	UnitPrice  float64           `gorm:"not null"`
	TotalPrice float64           `gorm:"not null"`
	CreatedAt  time.Time         `gorm:"autoCreateTime"`
	UpdatedAt  time.Time         `gorm:"autoUpdateTime"`
	DeletedAt  gorm.DeletedAt    `gorm:"index"`
	Options    []OrderItemOption `gorm:"foreignKey:OrderItemID;constraint:OnDelete:CASCADE"`
}

type OrderItemOption struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	OrderItemID string    `gorm:"not null;type:uuid;index"`
	OptionType  string    `gorm:"not null;type:varchar(100)"`
	OptionValue string    `gorm:"not null;type:varchar(255)"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (OrderItemOption) TableName() string {
	return "order_item_options"
}

func (oio *OrderItemOption) BeforeCreate(tx *gorm.DB) error {
	if oio.ID == "" {
		oio.ID = uuid.New().String()
	}
	return nil
}

func (OrderItem) TableName() string {
	return "order_items"
}

func (oi *OrderItem) BeforeCreate(tx *gorm.DB) error {
	if oi.ID == "" {
		oi.ID = uuid.New().String()
	}
	return nil
}

type Payment struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	OrderID       string         `gorm:"not null;type:uuid;uniqueIndex"`
	ExternalID    string         `gorm:"type:varchar(255);uniqueIndex"`
	Amount        float64        `gorm:"not null"`
	Status        string         `gorm:"not null;type:varchar(20);default:'pending'"`
	PaymentMethod string         `gorm:"not null;type:varchar(50)"`
	PixQrCode     string         `gorm:"type:text"`
	PixCopy       string         `gorm:"type:text"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (Payment) TableName() string {
	return "payments"
}

func (p *Payment) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
