package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Address struct {
	ID         string         `gorm:"primaryKey;type:uuid"`
	UserID     string         `gorm:"not null;type:uuid;index"`
	Name       string         `gorm:"not null;type:varchar(100)"`
	Street     string         `gorm:"not null;type:varchar(200)"`
	Number     string         `gorm:"not null;type:varchar(20)"`
	Complement string         `gorm:"type:varchar(100)"`
	District   string         `gorm:"not null;type:varchar(100)"`
	City       string         `gorm:"not null;type:varchar(100)"`
	State      string         `gorm:"not null;type:varchar(2)"`
	ZipCode    string         `gorm:"not null;type:varchar(10);index"`
	IsDefault  bool           `gorm:"default:false"`
	CreatedAt  time.Time      `gorm:"autoCreateTime"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (Address) TableName() string {
	return "addresses"
}

func (a *Address) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
