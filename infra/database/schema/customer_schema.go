package schema

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

const (
	CustomerCPFBlindScope  = "customers.cpf"
	CustomerCNPJBlindScope = "customers.cnpj"
)

type Customer struct {
	ID           string                  `gorm:"primaryKey;type:uuid"`
	UserID       sql.NullString          `gorm:"type:uuid;index"`
	Name         string                  `gorm:"not null;type:varchar(200)"`
	Email        string                  `gorm:"not null;type:varchar(200);uniqueIndex"`
	Gender       string                  `gorm:"type:varchar(20)"`
	BirthDate    string                  `gorm:"type:varchar(20)"`
	MobileNumber string                  `gorm:"type:varchar(32);index"`
	CPF          piigorm.EncryptedString `gorm:"column:cpf;type:bytea"`
	CPFBlind     piigorm.BlindIndex      `gorm:"column:cpf_blind;type:bytea;uniqueIndex:idx_customers_cpf_blind"`
	RG           string                  `gorm:"type:varchar(32)"`
	Nationality  string                  `gorm:"type:varchar(100)"`
	CNPJ         piigorm.EncryptedString `gorm:"column:cnpj;type:bytea"`
	CNPJBlind    piigorm.BlindIndex      `gorm:"column:cnpj_blind;type:bytea;index:idx_customers_cnpj_blind"`
	CreatedAt    time.Time               `gorm:"autoCreateTime"`
	UpdatedAt    time.Time               `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt          `gorm:"index"`
}

func (Customer) TableName() string {
	return "customers"
}

func (c *Customer) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
