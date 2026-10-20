package messageaction

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type resend struct {
	deps Deps
}

func (uc resend) Execute(ctx context.Context, by shared.Person, target conversation.MessageTarget, requestID string) (*conversation.Message, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, conversation.ErrClientRequestIDRequired
	}
	failed, err := uc.deps.load(by, target)
	if err != nil {
		return nil, err
	}
	if replacement, ok := uc.earlierResend(failed, requestID); ok {
		return replacement, nil
	}
	if err := conversation.CheckResend(failed, target.EntryID, target.EntryType); err != nil {
		return nil, err
	}

	in := failed.ResendInput()
	in.WorkspaceID = target.WorkspaceID
	in.SenderUserID = by.UserID
	in.RequestID = requestID
	sent, err := uc.deps.Send.Execute(ctx, in)
	if errors.Is(err, conversation.ErrSendRequestInFlight) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", conversation.ErrResendDeliveryFailed, err)
	}

	uc.foldInto(failed, sent)
	return sent, nil
}

func (uc resend) earlierResend(failed *conversation.Message, requestID string) (*conversation.Message, bool) {
	replacementID := failed.ResentAs()
	if replacementID == "" {
		return nil, false
	}
	replacement, err := uc.deps.Messages.GetByID(replacementID)
	if err != nil || replacement.ClientRequestID != requestID || replacement.EntryID != failed.EntryID {
		return nil, false
	}
	return replacement, true
}

func (uc resend) foldInto(failed, sent *conversation.Message) {
	if sent == nil {
		return
	}
	if err := failed.MarkResentAs(sent.ID); err != nil {
		log.Printf("[MessageAction] could not point failed message %s at its resend %s: %v", failed.ID, sent.ID, err)
		return
	}
	if err := uc.deps.Writer.Save(failed); err != nil {
		log.Printf("[MessageAction] could not store that failed message %s was resent as %s: %v", failed.ID, sent.ID, err)
	}
}
