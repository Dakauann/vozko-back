package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CEP struct {
	ID         string         `gorm:"primaryKey;type:uuid"`
	Cep        string         `gorm:"not null;type:varchar(10);uniqueIndex"`
	Logradouro string         `gorm:"type:varchar(200)"`
	Complement string         `gorm:"type:varchar(100)"`
	Bairro     string         `gorm:"type:varchar(100)"`
	Localidade string         `gorm:"type:varchar(100)"`
	Uf         string         `gorm:"type:varchar(2)"`
	CreatedAt  time.Time      `gorm:"autoCreateTime"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (CEP) TableName() string {
	return "ceps"
}

func (c *CEP) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
