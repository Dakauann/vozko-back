package container

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
	conversation_domain "vozko/domain/conversation"
)

type commentEscalationSender struct {
	send conversation_domain.OperatorSendUseCase
}

func (s commentEscalationSender) Send(ctx context.Context, in ca.EscalationDelivery) error {
	if s.send == nil {
		return fmt.Errorf("%w: the composer is not available", ca.ErrInvalidFilter)
	}
	entryID := strings.TrimSpace(in.RecipientID)
	entryType := strings.TrimSpace(in.RecipientKind)
	if entryID == "" || entryType == "" {
		return fmt.Errorf("%w: an escalation needs an existing conversation", ca.ErrInvalidFilter)
	}
	_, err := s.send.Execute(ctx, conversation_domain.OperatorSendInput{
		EntryID:      entryID,
		EntryType:    entryType,
		WorkspaceID:  in.WorkspaceID,
		SenderUserID: in.ActorUserID,
		Text:         in.Text,
		Signed:       true,
	})
	return err
}
