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

// NewDispatchUseCase wires the delivery path.
//
// The sender is conversation.OperatorSendUseCase — the same object the live
// composer uses — and that is the single most important decision in this
// feature. A scheduled message is an operator's message delivered later, so it
// must take the identical path: the same window re-check, the same signature
// format, the same media routing, the same status transition and timeline
// event. Any channel that can be replied to can be scheduled to, for free and
// forever, because there is no second send implementation to keep in step.
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

// Execute delivers one scheduled message, at most once.
//
// Safe to call any number of times for the same id, from any number of
// replicas: the claim below is a single conditional write, so exactly one
// caller proceeds and every other returns having done nothing. That is what
// lets the delayed queue be at-least-once and the sweep overlap it without
// either producing a duplicate message to the customer.
func (uc *dispatchUseCase) Execute(ctx context.Context, id string) error {
	message, err := uc.repo.ClaimForDispatch(id, uc.clock.Now())
	if err != nil {
		return err
	}
	if message == nil {
		// Cancelled, already sent, or claimed by another replica. All expected.
		return nil
	}

	return uc.deliver(ctx, message)
}

// DispatchClaimed delivers a message the caller has already claimed.
func (uc *dispatchUseCase) DispatchClaimed(ctx context.Context, message *sm.ScheduledMessage) error {
	if message == nil {
		return nil
	}
	return uc.deliver(ctx, message)
}

// deliver sends an already-claimed message.
func (uc *dispatchUseCase) deliver(ctx context.Context, message *sm.ScheduledMessage) error {
	entryType := string(message.EntryType)

	// The window is re-read rather than trusted from creation time. It cannot
	// have shrunk — every expiry this system reports only moves forward — but a
	// conversation can stop being replyable for reasons that are not a clock:
	// the contact blocks the bot, a linked device dies, a reply right is
	// revoked. Checking here buys a precise reason; the send path checks again
	// on its own, so this is a diagnosis, not the guard.
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

	// Written immediately, with nothing between it and the send that could
	// fail: this is the write that bounds the one window in which a crash could
	// leave a delivered message looking undelivered.
	if err := uc.repo.MarkSent(message.ID, sent.ID, uc.clock.Now()); err != nil {
		log.Printf("[scheduled_message] %s was DELIVERED as %s but could not be marked sent: %v",
			message.ID, sent.ID, err)
		return err
	}

	// Best-effort: the message is with the customer, so a broadcast failure must
	// not turn a delivered message into a failed one. The send use case has
	// already applied the conversation's own side effects.
	uc.broadcaster.BroadcastNewMessage(message.EntryID, entryType, sent)
	return nil
}

func (uc *dispatchUseCase) fail(message *sm.ScheduledMessage, reason sm.FailureReason, detail string) error {
	log.Printf("[scheduled_message] %s failed on %s (%s): %s=%s",
		message.ID, message.EntryID, message.EntryType, reason, detail)

	if err := uc.repo.MarkFailed(message.ID, reason, detail); err != nil {
		return err
	}
	// The failure is recorded and visible to the operator, so it is not the
	// caller's problem to retry: a queue consumer must ack, not redeliver.
	return nil
}

// classify turns a send error into the reason the operator will read.
//
// A provider error is deliberately NOT retried. We cannot tell a refused send
// from one that reached the customer before the connection dropped, and an
// unwanted duplicate is unrecoverable while a visible failure costs one click.
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
