package balance_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
)

func (uc *ConsumeEmailUseCase) QuoteSend(_ context.Context, workspaceID string, recipients, encodedAttachmentBytes int64) (EmailQuote, error) {
	return uc.messageQuote(workspaceID, recipients, emailsending.AttachmentChunks(encodedAttachmentBytes))
}

func (uc *ConsumeEmailUseCase) ChargeSend(_ context.Context, workspaceID, reference string, q EmailQuote) (bool, error) {
	if strings.TrimSpace(reference) == "" {
		return false, balance.ErrReferenceRequired
	}
	if q.Units <= 0 {
		return false, ErrEmailUnitsInvalid
	}
	if q.UnitMicros <= 0 {
		return false, fmt.Errorf("%w: the send needs its stored price", balance.ErrPriceUnavailable)
	}
	if err := uc.ensureSubscribed(workspaceID); err != nil {
		return false, err
	}
	return uc.debitQuote(workspaceID, reference, q, fmt.Sprintf("E-mail: envio pela API para %d destinatários", q.Units))
}

func (uc *ConsumeEmailUseCase) RefundSend(_ context.Context, workspaceID, reference string, q EmailQuote, units int64) error {
	if units > q.Units {
		return ErrEmailUnitsInvalid
	}
	return uc.refundUnits(workspaceID, reference, q, units, fmt.Sprintf("Reembolso: %d e-mails da API não enviados", units))
}

func (uc *ConsumeEmailUseCase) SendCharged(_ context.Context, reference string) (bool, error) {
	return uc.ledger.ExistsTransactionByReferenceID(reference)
}
