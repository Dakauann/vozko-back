package balance_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type EmailReceiveCharge struct {
	TotalMicros        int64
	BalanceAfterMicros int64
	Charged            bool
}

func EmailReceiveReference(messageKey string) string { return "email_receive:" + messageKey }

func (uc *ConsumeEmailUseCase) ChargeReceive(_ context.Context, workspaceID, messageKey string, sizeBytes int64) (EmailReceiveCharge, error) {
	if strings.TrimSpace(messageKey) == "" {
		return EmailReceiveCharge{}, balance.ErrReferenceRequired
	}
	price, err := uc.receivePrice(workspaceID, emailsending.BillableChunks(sizeBytes))
	if err != nil {
		return EmailReceiveCharge{}, err
	}
	ref := EmailReceiveReference(messageKey)
	transaction, err := DebitOnce(uc.ledger, ReferenceCharge{
		WorkspaceID:   workspaceID,
		ReferenceID:   ref,
		ServiceType:   balance.ServiceEmail,
		Price:         price,
		Description:   fmt.Sprintf("E-mail: resposta recebida (ref: %s)", ref),
		AllowNegative: true,
	})
	if err != nil {
		return EmailReceiveCharge{}, err
	}
	if transaction != nil {
		return EmailReceiveCharge{TotalMicros: price.PriceMicros, BalanceAfterMicros: transaction.BalanceAfter, Charged: true}, nil
	}
	current, err := uc.ledger.GetByWorkspaceID(workspaceID)
	if err != nil {
		return EmailReceiveCharge{}, fmt.Errorf("email: reading the balance after the recorded receive %s: %w", ref, err)
	}
	return EmailReceiveCharge{TotalMicros: price.PriceMicros, BalanceAfterMicros: current.Amount}, nil
}

func (uc *ConsumeEmailUseCase) receivePrice(workspaceID string, chunks int64) (workspace_pricing.PriceResult, error) {
	message, err := emailLinePrice(uc.pricer, workspaceID, workspace_pricing.EmailReceivePerMessage)
	if err != nil {
		return workspace_pricing.PriceResult{}, err
	}
	chunk, err := emailLinePrice(uc.pricer, workspaceID, workspace_pricing.EmailReceivePerChunk)
	if err != nil {
		return workspace_pricing.PriceResult{}, err
	}
	price := message.PriceMicros + chunk.PriceMicros*chunks
	cost := message.CostMicros + chunk.CostMicros*chunks
	return workspace_pricing.PriceResult{PriceMicros: price, CostMicros: cost, ProfitMicros: price - cost}, nil
}
