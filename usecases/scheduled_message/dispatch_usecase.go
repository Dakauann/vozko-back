package scheduled_message_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace"
	"vozko/domain/workspace/workspace_plan"
)

type dispatchUseCase struct {
	repo        sm.Repository
	windows     *windowService
	send        conversation.OperatorSendUseCase
	templates   wo.ConversationTemplateUseCase
	permission  templatePermission
	broadcaster conversation.EventBroadcaster
	clock       sm.Clock
}

func NewDispatchUseCase(
	repo sm.Repository,
	windows sm.WindowReader,
	send conversation.OperatorSendUseCase,
	templates wo.ConversationTemplateUseCase,
	permissions workspace.CheckAccessUseCase,
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
	if templates == nil {
		missing = append(missing, "template sender")
	}
	if permissions == nil {
		missing = append(missing, "permission check")
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
		templates:   templates,
		permission:  templatePermission{access: permissions},
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
	if message.Kind == sm.KindTemplate {
		return uc.deliverTemplate(ctx, message)
	}
	return uc.deliverText(ctx, message)
}

func (uc *dispatchUseCase) deliverText(ctx context.Context, message *sm.ScheduledMessage) error {
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

	if err := uc.markSent(message, sent.ID); err != nil {
		return err
	}

	uc.broadcaster.BroadcastNewMessage(message.EntryID, entryType, sent)
	return nil
}

func (uc *dispatchUseCase) deliverTemplate(ctx context.Context, message *sm.ScheduledMessage) error {
	creator := shared.Person{UserID: message.CreatedByUserID}
	if err := uc.permission.require(creator, message.WorkspaceID); err != nil {
		reason := sm.ReasonPermissionRevoked
		if !errors.Is(err, sm.ErrTemplatePermission) {
			reason = sm.ReasonDispatchInterrupted
		}
		return uc.fail(message, reason, err.Error())
	}

	if _, err := uc.templates.Send(ctx, templateSend(message)); err != nil {
		return uc.fail(message, classify(err), err.Error())
	}

	return uc.markSent(message, "")
}

func (uc *dispatchUseCase) markSent(message *sm.ScheduledMessage, sentMessageID string) error {
	if err := uc.repo.MarkSent(message.ID, sentMessageID, uc.clock.Now()); err != nil {
		log.Printf("[scheduled_message] %s was DELIVERED but could not be marked sent: %v", message.ID, err)
		return err
	}
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
		errors.Is(err, conversation.ErrNoAdapterForEntryType),
		errors.Is(err, wo.ErrConversationNotFound),
		errors.Is(err, wo.ErrBusinessPhoneNotFound),
		errors.Is(err, wo.ErrPhoneNotConnected):
		return sm.ReasonEntryUnavailable
	case errors.Is(err, wo.ErrLeadBlocked),
		errors.Is(err, wo.ErrWithinSpamWindow):
		return sm.ReasonContactIneligible
	case errors.Is(err, wo.ErrTemplateNotFound),
		errors.Is(err, wo.ErrTemplateForbidden),
		errors.Is(err, template.ErrTemplateNotSendable),
		errors.Is(err, template.ErrTemplatePhoneMismatch),
		errors.Is(err, template.ErrTemplateParamsMismatch):
		return sm.ReasonTemplateUnavailable
	case errors.Is(err, balance.ErrInsufficientBalance),
		errors.Is(err, balance.ErrBalanceNotFound):
		return sm.ReasonInsufficientBalance
	case errors.Is(err, template.ErrPricingUnavailable),
		errors.Is(err, balance.ErrPriceUnavailable),
		errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent),
		errors.Is(err, workspace_plan.ErrSubscriptionNotActive):
		return sm.ReasonBillingUnavailable
	case errors.Is(err, wo.ErrSendOutcomeUnknown):
		return sm.ReasonOutcomeUnknown
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
