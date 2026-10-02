package balance_usecase

import (
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

type referenceLookup interface {
	ExistsTransactionByReferenceID(referenceID string) (bool, error)
}

type ReferenceDebitLedger interface {
	referenceLookup
	DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error)
}

type ReferenceCreditLedger interface {
	referenceLookup
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
	charged, err := ledger.ExistsTransactionByReferenceID(c.ReferenceID)
	if err != nil {
		return nil, fmt.Errorf("balance: idempotency check for %s: %w", c.ReferenceID, err)
	}
	if charged {
		return nil, nil
	}
	reference := c.ReferenceID
	return ledger.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:   c.WorkspaceID,
		Amount:        c.Price.PriceMicros,
		ServiceType:   c.ServiceType,
		ReferenceID:   &reference,
		Description:   c.Description,
		CostMicros:    c.Price.CostMicros,
		ProfitMicros:  c.Price.ProfitMicros,
		AllowNegative: c.AllowNegative,
	})
}

func RefundOnce(ledger ReferenceCreditLedger, c ReferenceCharge) error {
	if c.Price.PriceMicros <= 0 {
		return fmt.Errorf("%w: cannot refund %s", balance.ErrPriceUnavailable, c.ServiceType)
	}
	reference := RefundReference(c.ReferenceID)
	refunded, err := ledger.ExistsTransactionByReferenceID(reference)
	if err != nil {
		return fmt.Errorf("balance: idempotency check for %s: %w", reference, err)
	}
	if refunded {
		return nil
	}
	description := c.RefundDescription
	if description == "" {
		description = "Reembolso: " + c.Description
	}
	_, err = ledger.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID:  c.WorkspaceID,
		Amount:       c.Price.PriceMicros,
		ServiceType:  c.ServiceType,
		ReferenceID:  &reference,
		Description:  description,
		CostMicros:   c.Price.CostMicros,
		ProfitMicros: -c.Price.ProfitMicros,
		IsRefund:     true,
	})
	return err
}
