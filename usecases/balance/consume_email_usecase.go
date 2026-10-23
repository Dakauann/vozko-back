package balance_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

var ErrEmailUnitsInvalid = errors.New("email: the number of emails or addresses is out of range")

type InsufficientEmailBalanceError struct {
	MissingMicros int64
}

func (e *InsufficientEmailBalanceError) Error() string {
	return fmt.Sprintf("%v: missing %d USD micros", balance.ErrInsufficientBalance, e.MissingMicros)
}

func (e *InsufficientEmailBalanceError) Unwrap() error {
	return balance.ErrInsufficientBalance
}

type EmailQuote struct {
	Units          int64 `json:"units"`
	UnitMicros     int64 `json:"unitMicros"`
	UnitCostMicros int64 `json:"unitCostMicros"`
	TotalMicros    int64 `json:"totalMicros"`
}

func (q EmailQuote) charge(units int64) workspace_pricing.PriceResult {
	price := q.UnitMicros * units
	cost := q.UnitCostMicros * units
	return workspace_pricing.PriceResult{PriceMicros: price, CostMicros: cost, ProfitMicros: price - cost}
}

type EmailLedger interface {
	ReferenceDebitLedger
	ReferenceCreditLedger
	GetByWorkspaceID(workspaceID string) (*balance.Balance, error)
	ExistsTransactionByReferenceID(referenceID string) (bool, error)
}

type EmailPricer interface {
	PriceEmail(workspaceID string, line workspace_pricing.EmailPriceLine) (workspace_pricing.PriceResult, error)
}

type ConsumeEmailUseCase struct {
	ledger  EmailLedger
	pricer  EmailPricer
	checker workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
}

func NewConsumeEmailUseCase(ledger EmailLedger, pricer EmailPricer, checker workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase) *ConsumeEmailUseCase {
	return &ConsumeEmailUseCase{ledger: ledger, pricer: pricer, checker: checker}
}

func EmailCampaignBatchReference(campaignID string, batchNo int) string {
	return fmt.Sprintf("email_campaign:%s:batch:%d", campaignID, batchNo)
}

func EmailMessageReference(messageKey string) string { return "email_message:" + messageKey }

func (uc *ConsumeEmailUseCase) quote(workspaceID string, line workspace_pricing.EmailPriceLine, units int64) (EmailQuote, error) {
	if units <= 0 {
		return EmailQuote{}, ErrEmailUnitsInvalid
	}
	if err := uc.ensureSubscribed(workspaceID); err != nil {
		return EmailQuote{}, err
	}
	price, err := emailLinePrice(uc.pricer, workspaceID, line)
	if err != nil {
		return EmailQuote{}, err
	}
	return EmailQuote{Units: units, UnitMicros: price.PriceMicros, UnitCostMicros: price.CostMicros, TotalMicros: price.PriceMicros * units}, nil
}

func (uc *ConsumeEmailUseCase) QuoteRun(_ context.Context, workspaceID string, recipients int64) (EmailQuote, error) {
	return uc.quote(workspaceID, workspace_pricing.EmailSendPerMessage, recipients)
}

func (uc *ConsumeEmailUseCase) ensureSubscribed(workspaceID string) error {
	if uc.checker == nil {
		return fmt.Errorf("current subscription checker is required")
	}
	_, err := uc.checker.Execute(workspaceID)
	return err
}

func (uc *ConsumeEmailUseCase) ChargeBatch(_ context.Context, workspaceID, campaignID string, batchNo int, q EmailQuote) error {
	if strings.TrimSpace(campaignID) == "" {
		return balance.ErrReferenceRequired
	}
	if batchNo < 1 || q.Units <= 0 {
		return ErrEmailUnitsInvalid
	}
	if q.UnitMicros <= 0 {
		return fmt.Errorf("%w: the batch needs its stored price", balance.ErrPriceUnavailable)
	}
	if err := uc.ensureSubscribed(workspaceID); err != nil {
		return err
	}
	_, err := uc.debitQuote(workspaceID, EmailCampaignBatchReference(campaignID, batchNo), q,
		fmt.Sprintf("E-mail: campanha, lote %d com %d envios", batchNo, q.Units))
	return err
}

func (uc *ConsumeEmailUseCase) BatchCharged(_ context.Context, campaignID string, batchNo int) (bool, error) {
	return uc.ledger.ExistsTransactionByReferenceID(EmailCampaignBatchReference(campaignID, batchNo))
}

func (uc *ConsumeEmailUseCase) RefundBatch(_ context.Context, workspaceID, campaignID string, batchNo int, q EmailQuote, units int64) error {
	if units > q.Units {
		return ErrEmailUnitsInvalid
	}
	return uc.refundUnits(workspaceID, EmailCampaignBatchReference(campaignID, batchNo), q, units,
		fmt.Sprintf("Reembolso: %d e-mails do lote %d da campanha não enviados", units, batchNo))
}
func (uc *ConsumeEmailUseCase) ChargeMessage(_ context.Context, workspaceID, messageKey string) (EmailQuote, error) {
	return uc.chargeUnits(workspaceID, workspace_pricing.EmailSendPerMessage, messageKey, EmailMessageReference, 1, "E-mail: envio avulso")
}

type EmailMessageCharge struct {
	Key      string
	Quote    EmailQuote
	Recorded bool
}

func messageRoundKey(messageKey string, round int) string {
	if round == 1 {
		return messageKey
	}
	return fmt.Sprintf("%s#%d", messageKey, round)
}

func (uc *ConsumeEmailUseCase) ChargeMessageRound(_ context.Context, workspaceID, messageKey string) (EmailMessageCharge, error) {
	if strings.TrimSpace(messageKey) == "" {
		return EmailMessageCharge{}, balance.ErrReferenceRequired
	}
	for round := 1; ; round++ {
		key := messageRoundKey(messageKey, round)
		refunded, err := uc.ledger.ExistsTransactionByReferenceID(RefundReference(EmailMessageReference(key)))
		if err != nil {
			return EmailMessageCharge{}, fmt.Errorf("email: reading the refund of %s: %w", key, err)
		}
		if refunded {
			continue
		}
		quote, recorded, err := uc.debitUnits(workspaceID, workspace_pricing.EmailSendPerMessage, key, EmailMessageReference, 1, "E-mail: envio avulso")
		if err != nil {
			return EmailMessageCharge{}, err
		}
		return EmailMessageCharge{Key: key, Quote: quote, Recorded: recorded}, nil
	}
}

func (uc *ConsumeEmailUseCase) RefundMessage(_ context.Context, workspaceID, messageKey string, charge EmailQuote) error {
	return uc.refundUnits(workspaceID, EmailMessageReference(messageKey), charge, 1, "Reembolso: e-mail avulso não enviado")
}

func (uc *ConsumeEmailUseCase) chargeUnits(workspaceID string, line workspace_pricing.EmailPriceLine, key string, reference func(string) string, units int64, description string) (EmailQuote, error) {
	q, _, err := uc.debitUnits(workspaceID, line, key, reference, units, description)
	return q, err
}

func (uc *ConsumeEmailUseCase) debitUnits(workspaceID string, line workspace_pricing.EmailPriceLine, key string, reference func(string) string, units int64, description string) (EmailQuote, bool, error) {
	if strings.TrimSpace(key) == "" {
		return EmailQuote{}, false, balance.ErrReferenceRequired
	}
	q, err := uc.quote(workspaceID, line, units)
	if err != nil {
		return EmailQuote{}, false, err
	}
	recorded, err := uc.debitQuote(workspaceID, reference(key), q, description)
	if err != nil {
		return EmailQuote{}, false, err
	}
	return q, recorded, nil
}

func (uc *ConsumeEmailUseCase) debitQuote(workspaceID, ref string, q EmailQuote, description string) (bool, error) {
	transaction, err := DebitOnce(uc.ledger, ReferenceCharge{
		WorkspaceID: workspaceID,
		ReferenceID: ref,
		ServiceType: balance.ServiceEmail,
		Price:       q.charge(q.Units),
		Description: fmt.Sprintf("%s (ref: %s)", description, ref),
	})
	if err != nil {
		if errors.Is(err, balance.ErrInsufficientBalance) || errors.Is(err, balance.ErrBalanceNotFound) {
			return false, &InsufficientEmailBalanceError{MissingMicros: uc.missing(workspaceID, q.Units*q.UnitMicros)}
		}
		return false, err
	}
	return transaction != nil, nil
}

func (uc *ConsumeEmailUseCase) refundUnits(workspaceID, ref string, charge EmailQuote, units int64, description string) error {
	if units < 0 {
		return ErrEmailUnitsInvalid
	}
	if units == 0 {
		return nil
	}
	if charge.UnitMicros <= 0 {
		return fmt.Errorf("%w: the refund needs the price charged", balance.ErrPriceUnavailable)
	}
	return RefundOnce(uc.ledger, ReferenceCharge{
		WorkspaceID:       workspaceID,
		ReferenceID:       ref,
		ServiceType:       balance.ServiceEmail,
		Price:             charge.charge(units),
		RefundDescription: fmt.Sprintf("%s (ref: %s)", description, ref),
	})
}

func (uc *ConsumeEmailUseCase) missing(workspaceID string, total int64) int64 {
	current, err := uc.ledger.GetByWorkspaceID(workspaceID)
	if err != nil || current == nil {
		return total
	}
	return max(total-max(current.Amount, 0), 1)
}
