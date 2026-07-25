package address

import (
	"errors"
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
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	Name       string    `json:"name"`
	Street     string    `json:"street"`
	Number     string    `json:"number"`
	Complement string    `json:"complement,omitempty"`
	District   string    `json:"district"`
	City       string    `json:"city"`
	State      string    `json:"state"`
	ZipCode    string    `json:"zipCode"`
	IsDefault  bool      `json:"isDefault"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
