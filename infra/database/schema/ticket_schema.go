package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Ticket struct {
	ID             string           `gorm:"primaryKey;type:uuid"`
	OrderID        string           `gorm:"not null;type:uuid;uniqueIndex"`
	UserID         string           `gorm:"not null;type:uuid;index"`
	Status         string           `gorm:"not null;type:varchar(40)"`
	TrackingCode   *string          `gorm:"type:varchar(100)"`
	LastStatusBy   *string          `gorm:"type:uuid"`
	LastStatusAt   *time.Time       `gorm:""`
	LastRevertedBy *string          `gorm:"type:uuid"`
	CreatedAt      time.Time        `gorm:"autoCreateTime"`
	UpdatedAt      time.Time        `gorm:"autoUpdateTime"`
	DeletedAt      gorm.DeletedAt   `gorm:"index"`
	Documents      []TicketDocument `gorm:"foreignKey:TicketID;constraint:OnDelete:CASCADE"`
}

func (Ticket) TableName() string {
	return "tickets"
}

func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

type TicketDocument struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	TicketID    string    `gorm:"not null;type:uuid;index"`
	Type        string    `gorm:"not null;type:varchar(40)"`
	FileName    string    `gorm:"type:varchar(255)"`
	ContentType string    `gorm:"type:varchar(100)"`
	URL         string    `gorm:"type:text"`
	UploadedBy  string    `gorm:"type:uuid"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (TicketDocument) TableName() string {
	return "ticket_documents"
}

func (td *TicketDocument) BeforeCreate(tx *gorm.DB) error {
	if td.ID == "" {
		td.ID = uuid.New().String()
	}
	return nil
}
