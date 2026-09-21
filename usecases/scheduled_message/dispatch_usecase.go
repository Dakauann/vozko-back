package scheduled_message_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
)

type dispatchUseCase struct {
	repo        sm.Repository
	windows     *windowService
	send        conversation.OperatorSendUseCase
	broadcaster conversation.EventBroadcaster
	clock       sm.Clock
}

func NewDispatchUseCase(
	repo sm.Repository,
	windows sm.WindowReader,
	send conversation.OperatorSendUseCase,
	broadcaster conversation.EventBroadcaster,
	clock sm.Clock,
) (sm.DispatchUseCase, error) {
	windowSvc, err := newWindowService(windows, clock)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	if repo == nil {
		missing = append(missing, "repository")
	}
	if send == nil {
		missing = append(missing, "operator send use case")
	}
	if broadcaster == nil {
		missing = append(missing, "event broadcaster")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message dispatch use case: missing %s", strings.Join(missing, ", "))
	}

	return &dispatchUseCase{
		repo:        repo,
		windows:     windowSvc,
		send:        send,
		broadcaster: broadcaster,
		clock:       clock,
	}, nil
}

func (uc *dispatchUseCase) Execute(ctx context.Context, id string) error {
	message, err := uc.repo.ClaimForDispatch(id, uc.clock.Now())
	if err != nil {
		return err
	}
	if message == nil {
		return nil
	}

	return uc.deliver(ctx, message)
}

func (uc *dispatchUseCase) DispatchClaimed(ctx context.Context, message *sm.ScheduledMessage) error {
	if message == nil {
		return nil
	}
	return uc.deliver(ctx, message)
}

func (uc *dispatchUseCase) deliver(ctx context.Context, message *sm.ScheduledMessage) error {
	entryType := string(message.EntryType)

	if !uc.windows.IsOpen(message.EntryID, entryType) {
		return uc.fail(message, sm.ReasonWindowClosed, "the messaging window was closed at delivery time")
	}

	sent, err := uc.send.Execute(ctx, conversation.OperatorSendInput{
		EntryID:          message.EntryID,
		EntryType:        entryType,
		WorkspaceID:      message.WorkspaceID,
		SenderUserID:     message.CreatedByUserID,
		Text:             message.Text,
		MediaID:          deref(message.MediaID),
		MediaType:        deref(message.MediaType),
		ReplyToMessageID: deref(message.ReplyToMessageID),
		Signed:           message.Signed,
	})
	if err != nil {
		return uc.fail(message, classify(err), err.Error())
	}

	if err := uc.repo.MarkSent(message.ID, sent.ID, uc.clock.Now()); err != nil {
		log.Printf("[scheduled_message] %s was DELIVERED as %s but could not be marked sent: %v",
			message.ID, sent.ID, err)
		return err
	}

	uc.broadcaster.BroadcastNewMessage(message.EntryID, entryType, sent)
	return nil
}

func (uc *dispatchUseCase) fail(message *sm.ScheduledMessage, reason sm.FailureReason, detail string) error {
	log.Printf("[scheduled_message] %s failed on %s (%s): %s=%s",
		message.ID, message.EntryID, message.EntryType, reason, detail)

	if err := uc.repo.MarkFailed(message.ID, reason, detail); err != nil {
		return err
	}
	return nil
}

func classify(err error) sm.FailureReason {
	switch {
	case errors.Is(err, conversation.ErrWindowClosed),
		errors.Is(err, conversation.ErrOutboundWindowClosed):
		return sm.ReasonWindowClosed
	case errors.Is(err, conversation.ErrConversationNotFound),
		errors.Is(err, conversation.ErrEntryTypeInvalid),
		errors.Is(err, conversation.ErrNoAdapterForEntryType):
		return sm.ReasonEntryUnavailable
	default:
		return sm.ReasonProviderError
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ sm.DispatchUseCase = (*dispatchUseCase)(nil)
