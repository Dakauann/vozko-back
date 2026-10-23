package balance_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

func (uc *ConsumeEmailUseCase) ChargeMessageWithAttachments(ctx context.Context, workspaceID, messageKey string, encodedAttachmentBytes int64) (EmailQuote, error) {
	chunks := emailsending.AttachmentChunks(encodedAttachmentBytes)
	if chunks == 0 {
		return uc.ChargeMessage(ctx, workspaceID, messageKey)
	}
	if strings.TrimSpace(messageKey) == "" {
		return EmailQuote{}, balance.ErrReferenceRequired
	}
	q, err := uc.messageQuote(workspaceID, 1, chunks)
	if err != nil {
		return EmailQuote{}, err
	}
	if _, err := uc.debitQuote(workspaceID, EmailMessageReference(messageKey), q,
		fmt.Sprintf("E-mail: envio avulso com anexos, %d blocos de %d KB", chunks, emailsending.OutboundChunkBytes>>10)); err != nil {
		return EmailQuote{}, err
	}
	return q, nil
}

func (uc *ConsumeEmailUseCase) messageQuote(workspaceID string, units, chunks int64) (EmailQuote, error) {
	q, err := uc.quote(workspaceID, workspace_pricing.EmailSendPerMessage, units)
	if err != nil || chunks == 0 {
		return q, err
	}
	chunk, err := emailLinePrice(uc.pricer, workspaceID, workspace_pricing.EmailSendPerChunk)
	if err != nil {
		return EmailQuote{}, err
	}
	q.UnitMicros += chunk.PriceMicros * chunks
	q.UnitCostMicros += chunk.CostMicros * chunks
	q.TotalMicros = q.UnitMicros * q.Units
	return q, nil
}
