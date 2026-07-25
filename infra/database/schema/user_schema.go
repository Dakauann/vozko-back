package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

const (
	UserCPFBlindScope  = "users.cpf"
	UserCNPJBlindScope = "users.cnpj"
)

type User struct {
	ID            string                  `gorm:"primaryKey;type:uuid"`
	Username      string                  `gorm:"not null;size:255"`
	Email         string                  `gorm:"uniqueIndex;not null;size:255"`
	Password      string                  `gorm:"not null"`
	Picture       string                  `gorm:"size:500"`
	Role          string                  `gorm:"not null;size:50;default:'user'"`
	CustomerType  string                  `gorm:"not null;size:50;default:'individual'"`
	CPF           piigorm.EncryptedString `gorm:"column:cpf;type:bytea"`
	CPFBlind      piigorm.BlindIndex      `gorm:"column:cpf_blind;type:bytea;uniqueIndex:idx_users_cpf_blind"`
	CNPJ          piigorm.EncryptedString `gorm:"column:cnpj;type:bytea"`
	CNPJBlind     piigorm.BlindIndex      `gorm:"column:cnpj_blind;type:bytea;uniqueIndex:idx_users_cnpj_blind"`
	EmailVerified bool                    `gorm:"default:false"`
	TokenVersion  int                     `gorm:"not null;default:0"`
	DisabledAt    *time.Time              `gorm:"index"`
	CreatedAt     time.Time               `gorm:"autoCreateTime"`
	UpdatedAt     time.Time               `gorm:"autoUpdateTime"`
}

func (User) TableName() string {
	return "users"
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}
