package payment

import (
	"errors"
	"strings"
	"time"
)

type SplitType string

const (
	SplitTypeSupplier SplitType = "supplier"
	SplitTypeReseller SplitType = "reseller"
	SplitTypeOwner    SplitType = "owner"
)

type SplitProvider string

const (
	SplitProviderAsaas SplitProvider = "ASAAS"
)

type PaymentSplit struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Type        SplitType     `json:"type"`
	Provider    SplitProvider `json:"provider"`
	WalletID    string        `json:"walletId"`
	Percentage  float64       `json:"percentageSplit,omitempty"`
	FixedAmount float64       `json:"fixedAmount,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	DeletedAt   *time.Time    `json:"-"`
}

func NormalizeSplitType(raw string) (SplitType, error) {
	switch strings.ToLower(raw) {
	case string(SplitTypeSupplier):
		return SplitTypeSupplier, nil
	case string(SplitTypeReseller):
		return SplitTypeReseller, nil
	case string(SplitTypeOwner):
		return SplitTypeOwner, nil
	default:
		return "", ErrInvalidSplitType
	}
}

func NormalizeSplitProvider(raw string) (SplitProvider, error) {
	switch strings.ToUpper(raw) {
	case string(SplitProviderAsaas):
		return SplitProviderAsaas, nil
	default:
		return "", ErrInvalidSplitProvider
	}
}

// TODO: remove from there, this is wrong as this is in the domain, and domain should not have validation logic
func ValidateSplit(split *PaymentSplit) error {
	if split.Name == "" {
		return ErrPaymentSplitNameRequired
	}
	if split.WalletID == "" {
		return ErrPaymentSplitWalletRequired
	}
	if _, err := NormalizeSplitProvider(string(split.Provider)); err != nil {
		return err
	}

	if split.Type == "" {
		return ErrInvalidSplitType
	}

	switch split.Type {
	case SplitTypeOwner, SplitTypeReseller:
		if split.Percentage <= 0 {
			return ErrInvalidSplitPercentage
		}
		if split.Percentage > 100 {
			return ErrInvalidSplitPercentage
		}
	case SplitTypeSupplier:
		if split.Percentage < 0 {
			return ErrInvalidSplitPercentage
		}
		if split.FixedAmount < 0 {
			return ErrInvalidSplitFixedAmount
		}
	default:
		return ErrInvalidSplitType
	}

	return nil
}

var (
	ErrInvalidSplitType              = errors.New("invalid split type")
	ErrInvalidSplitProvider          = errors.New("invalid split provider")
	ErrInvalidSplitPercentage        = errors.New("invalid split percentage")
	ErrAtLeastOneOfPercentageOrFixed = errors.New("at least one of percentageSplit or fixedAmountSplit must be provided")
	ErrInvalidSplitFixedAmount       = errors.New("invalid split fixed amount")
	ErrPaymentSplitNameRequired      = errors.New("payment split name is required")
	ErrPaymentSplitWalletRequired    = errors.New("payment split wallet id is required")
	ErrPaymentSplitNotFound          = errors.New("payment split not found")
)
