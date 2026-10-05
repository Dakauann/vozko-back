package balance_usecase

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/balance"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type ReferenceCharge struct {
	WorkspaceID       string
	ReferenceID       string
	ServiceType       balance.ServiceType
	Price             workspace_pricing.PriceResult
	Description       string
	RefundDescription string
	AllowNegative     bool
}

type ReferenceDebitLedger interface {
	DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error)
}

type ReferenceCreditLedger interface {
	CreditBalance(params balance.CreditBalanceInput) (*balance.Transaction, error)
}

func RefundReference(referenceID string) string { return "refund:" + referenceID }

func DebitOnce(ledger ReferenceDebitLedger, c ReferenceCharge) (*balance.Transaction, error) {
	if strings.TrimSpace(c.ReferenceID) == "" {
		return nil, fmt.Errorf("balance: a charge needs a reference")
	}
	if c.Price.PriceMicros <= 0 {
		return nil, fmt.Errorf("%w: %s", balance.ErrPriceUnavailable, c.ServiceType)
	}
	reference := c.ReferenceID
	transaction, err := ledger.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:      c.WorkspaceID,
		Amount:           c.Price.PriceMicros,
		ServiceType:      c.ServiceType,
		ReferenceID:      &reference,
		Description:      c.Description,
		CostMicros:       c.Price.CostMicros,
		ProfitMicros:     c.Price.ProfitMicros,
		AllowNegative:    c.AllowNegative,
		OncePerReference: true,
	})
	if errors.Is(err, balance.ErrReferenceAlreadyRecorded) {
		return nil, nil
	}
	return transaction, err
}

func RefundOnce(ledger ReferenceCreditLedger, c ReferenceCharge) error {
	if c.Price.PriceMicros <= 0 {
		return fmt.Errorf("%w: cannot refund %s", balance.ErrPriceUnavailable, c.ServiceType)
	}
	reference := RefundReference(c.ReferenceID)
	description := c.RefundDescription
	if description == "" {
		description = "Reembolso: " + c.Description
	}
	_, err := ledger.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID:      c.WorkspaceID,
		Amount:           c.Price.PriceMicros,
		ServiceType:      c.ServiceType,
		ReferenceID:      &reference,
		Description:      description,
		CostMicros:       c.Price.CostMicros,
		ProfitMicros:     -c.Price.ProfitMicros,
		IsRefund:         true,
		OncePerReference: true,
	})
	if errors.Is(err, balance.ErrReferenceAlreadyRecorded) {
		return nil
	}
	return err
}
