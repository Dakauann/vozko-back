package carriermessaging_usecase

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/carriermessaging"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
)

var ErrQuantityInvalid = errors.New("carrier messaging: a charge needs a positive quantity")

type Pricer interface {
	PriceRCS(workspaceID, service string) (workspace_pricing.PriceResult, error)
	PriceSMS(workspaceID, service string) (workspace_pricing.PriceResult, error)
}

type Ledger interface {
	balance_usecase.ReferenceDebitLedger
	balance_usecase.ReferenceCreditLedger
}

type Charges struct {
	pricer Pricer
	ledger Ledger
}

func NewCharges(pricer Pricer, ledger Ledger) *Charges {
	return &Charges{pricer: pricer, ledger: ledger}
}

func LedgerService(channel carriermessaging.Channel) balance.ServiceType {
	if channel == carriermessaging.ChannelSMS {
		return balance.ServiceSMS
	}
	return balance.ServiceRCS
}

func (c *Charges) Price(workspaceID string, kind carriermessaging.ChargeKind) (workspace_pricing.PriceResult, error) {
	var price workspace_pricing.PriceResult
	var err error
	switch kind.Channel() {
	case carriermessaging.ChannelRCS:
		price, err = c.pricer.PriceRCS(workspaceID, kind.PricingService())
	case carriermessaging.ChannelSMS:
		price, err = c.pricer.PriceSMS(workspaceID, kind.PricingService())
	default:
		return workspace_pricing.PriceResult{}, fmt.Errorf("%w: unknown charge %s", carriermessaging.ErrPriceUnavailable, kind)
	}
	if errors.Is(err, workspace_pricing.ErrPricingItemNotFound) || (err == nil && price.PriceMicros <= 0) {
		return workspace_pricing.PriceResult{}, fmt.Errorf("%w: %s", carriermessaging.ErrPriceUnavailable, kind)
	}
	if err != nil {
		return workspace_pricing.PriceResult{}, err
	}
	return price, nil
}

func (c *Charges) Amount(workspaceID string, kind carriermessaging.ChargeKind, quantity int) (int64, error) {
	if quantity <= 0 {
		return 0, ErrQuantityInvalid
	}
	price, err := c.Price(workspaceID, kind)
	if err != nil {
		return 0, err
	}
	return price.PriceMicros * int64(quantity), nil
}

func (c *Charges) Debit(workspaceID string, kind carriermessaging.ChargeKind, key string, quantity int, allowNegative bool) error {
	charge, err := c.charge(workspaceID, kind, key, quantity)
	if err != nil {
		return err
	}
	charge.AllowNegative = allowNegative
	_, err = balance_usecase.DebitOnce(c.ledger, charge)
	return err
}

func (c *Charges) Refund(workspaceID string, kind carriermessaging.ChargeKind, key string, quantity int) error {
	charge, err := c.charge(workspaceID, kind, key, quantity)
	if err != nil {
		return err
	}
	return balance_usecase.RefundOnce(c.ledger, charge)
}

func (c *Charges) charge(workspaceID string, kind carriermessaging.ChargeKind, key string, quantity int) (balance_usecase.ReferenceCharge, error) {
	if strings.TrimSpace(key) == "" {
		return balance_usecase.ReferenceCharge{}, fmt.Errorf("carrier messaging: a charge needs a key")
	}
	if quantity <= 0 {
		return balance_usecase.ReferenceCharge{}, ErrQuantityInvalid
	}
	unit, err := c.Price(workspaceID, kind)
	if err != nil {
		return balance_usecase.ReferenceCharge{}, err
	}
	n := int64(quantity)
	reference := carriermessaging.ChargeReference(kind, key)
	label := strings.ToUpper(string(kind.Channel())) + " " + kind.PricingService()
	return balance_usecase.ReferenceCharge{
		WorkspaceID: workspaceID,
		ReferenceID: reference,
		ServiceType: LedgerService(kind.Channel()),
		Price: workspace_pricing.PriceResult{
			CostMicros: unit.CostMicros * n, PriceMicros: unit.PriceMicros * n, ProfitMicros: unit.ProfitMicros * n,
		},
		Description:       fmt.Sprintf("%s x%d (ref: %s)", label, quantity, reference),
		RefundDescription: fmt.Sprintf("Reembolso: %s não entregue (ref: %s)", label, reference),
	}, nil
}
