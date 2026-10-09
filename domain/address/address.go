package address

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrAddressNotFound     = errors.New("address not found")
	ErrInvalidAddress      = errors.New("invalid address data")
	ErrMaxAddressesReached = errors.New("maximum number of addresses reached")
)

const (
	MaxAddressesPerUser = 10
)

type Address struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Postal
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a Address) Missing() []Field {
	missing := a.Postal.Missing()
	if strings.TrimSpace(a.Name) == "" {
		missing = append([]Field{FieldName}, missing...)
	}
	return missing
}
