package advertising

import (
	"fmt"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
)

const feeCurrency = "USD"

type Fee struct {
	PriceMicros int64
	Currency    string
}

func (f Fee) Times(quantity int) Fee {
	return Fee{PriceMicros: f.PriceMicros * int64(quantity), Currency: f.Currency}
}

type FeeCharger interface {
	Quote(workspaceID string) (Fee, error)
	Charge(workspaceID, reference string, quantity int) (Fee, error)
	Refund(workspaceID, reference string, chargedMicros int64) error
}

type feePricer interface {
	PriceAdvertising(workspaceID, service string) (workspace_pricing.PriceResult, error)
}

type feeLedger interface {
	balance_usecase.ReferenceDebitLedger
	balance_usecase.ReferenceCreditLedger
}

type ledgerFeeCharger struct {
	pricer        feePricer
	ledger        feeLedger
	subscriptions workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
}

func NewFeeCharger(pricer feePricer, ledger feeLedger, subscriptions workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase) FeeCharger {
	return &ledgerFeeCharger{pricer: pricer, ledger: ledger, subscriptions: subscriptions}
}

func (c *ledgerFeeCharger) ensureSubscription(workspaceID string) error {
	if c.subscriptions == nil {
		return fmt.Errorf("ads: subscription checker is required")
	}
	_, err := c.subscriptions.Execute(workspaceID)
	return err
}

func (c *ledgerFeeCharger) charge(workspaceID, reference string) (balance_usecase.ReferenceCharge, error) {
	price, err := c.pricer.PriceAdvertising(workspaceID, workspace_pricing.AdvertisingServicePublishedAd)
	if err != nil {
		return balance_usecase.ReferenceCharge{}, err
	}
	return balance_usecase.ReferenceCharge{
		WorkspaceID:       workspaceID,
		ReferenceID:       reference,
		ServiceType:       balance.ServiceAdvertising,
		Price:             price,
		Description:       fmt.Sprintf("Anúncio publicado na Meta (ref: %s)", reference),
		RefundDescription: fmt.Sprintf("Reembolso: anúncio não publicado (ref: %s)", reference),
	}, nil
}

func (c *ledgerFeeCharger) Quote(workspaceID string) (Fee, error) {
	if err := c.ensureSubscription(workspaceID); err != nil {
		return Fee{}, err
	}
	charge, err := c.charge(workspaceID, "")
	if err != nil {
		return Fee{}, err
	}
	return Fee{PriceMicros: charge.Price.PriceMicros, Currency: feeCurrency}, nil
}

func (c *ledgerFeeCharger) Charge(workspaceID, reference string, quantity int) (Fee, error) {
	if quantity <= 0 {
		return Fee{}, fmt.Errorf("ads: nothing to charge")
	}
	if err := c.ensureSubscription(workspaceID); err != nil {
		return Fee{}, err
	}
	charge, err := c.charge(workspaceID, reference)
	if err != nil {
		return Fee{}, err
	}
	charge.Price = scaled(charge.Price, quantity)
	charge.Description = fmt.Sprintf("%d anúncio(s) publicado(s) na Meta (ref: %s)", quantity, reference)
	if _, err := balance_usecase.DebitOnce(c.ledger, charge); err != nil {
		return Fee{}, err
	}
	return Fee{PriceMicros: charge.Price.PriceMicros, Currency: feeCurrency}, nil
}

func (c *ledgerFeeCharger) Refund(workspaceID, reference string, chargedMicros int64) error {
	charge, err := c.charge(workspaceID, reference)
	if err != nil {
		return err
	}
	charge.Price.ProfitMicros = chargedMicros - charge.Price.CostMicros
	charge.Price.PriceMicros = chargedMicros
	return balance_usecase.RefundOnce(c.ledger, charge)
}

func scaled(p workspace_pricing.PriceResult, quantity int) workspace_pricing.PriceResult {
	q := int64(quantity)
	return workspace_pricing.PriceResult{CostMicros: p.CostMicros * q, PriceMicros: p.PriceMicros * q, ProfitMicros: p.ProfitMicros * q}
}
