package container

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/comment_analysis"
	conversation_domain "vozko/domain/conversation"
)

// commentEscalationSender adapts the CRM composer onto the narrow port the
// comment engine declares for forwarding a comment (§3).
//
// It lives in the composition root for the same reason pipelineStageSeeder and
// conversationFunnelLister do: comment analysis and conversations are separate
// aggregates that must not import each other, and this is the only place
// allowed to know both sides.
//
// The choice it encodes, and the reason it is the safe one: an escalation goes
// into a conversation the workspace ALREADY has open, through the same
// OperatorSendUseCase the live composer uses. So there is no cold outbound to a
// stranger, no template to buy, no 24-hour window to reason about, and no
// second send implementation to keep in step with the first. The channel is
// whichever one that conversation already runs on, which is also the channel
// the recipient already answers on.
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
		// Signed: the recipient must be able to tell that a person forwarded
		// this to them, not that the system decided to message them.
		Signed: true,
	})
	return err
}
