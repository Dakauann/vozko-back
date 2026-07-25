package user

import "time"

type Role string
type CustomerType string

const (
	CustomerTypeIndividual CustomerType = "individual"
	CustomerTypeCompany    CustomerType = "company"
)

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID            string
	Username      string
	Email         string
	Password      string
	Picture       string
	Role          Role
	CustomerType  CustomerType
	CPF           string
	CNPJ          string
	EmailVerified bool
	TokenVersion  int
	DisabledAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
