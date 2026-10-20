package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/user"
)

type operatorSendUseCase struct {
	sender    conversation.MessageSender
	users     user.UserRepository
	finalizer conversation.OperatorSendFinalizer
	billing   conversation.ServiceMessageBilling
	requests  conversation.SendRequestLedger
}

func NewOperatorSendUseCase(
	sender conversation.MessageSender,
	users user.UserRepository,
	finalizer conversation.OperatorSendFinalizer,
	billing conversation.ServiceMessageBilling,
	requests conversation.SendRequestLedger,
) (conversation.OperatorSendUseCase, error) {
	missing := []string{}
	if sender == nil {
		missing = append(missing, "message sender")
	}
	if users == nil {
		missing = append(missing, "user repository")
	}
	if finalizer == nil {
		missing = append(missing, "operator send finalizer")
	}
	if billing == nil {
		missing = append(missing, "service message billing")
	}
	if requests == nil {
		missing = append(missing, "send request ledger")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("operator send use case: missing %s", strings.Join(missing, ", "))
	}

	return &operatorSendUseCase{sender: sender, users: users, finalizer: finalizer, billing: billing, requests: requests}, nil
}

func (uc *operatorSendUseCase) Execute(ctx context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	in.EntryID = strings.TrimSpace(in.EntryID)
	in.EntryType = strings.TrimSpace(in.EntryType)
	in.Text = strings.TrimSpace(in.Text)

	if in.EntryID == "" {
		return nil, conversation.ErrEntryIDRequired
	}
	if !shared.EntryType(in.EntryType).IsKnown() {
		return nil, conversation.ErrEntryTypeInvalid
	}
	if in.Text == "" && in.MediaID == "" && in.Buttons == nil {
		return nil, conversation.ErrMessageContentRequired
	}

	request, deduplicated, err := conversation.NewSendRequest(in.EntryType, in.EntryID, in.RequestID)
	if err != nil {
		return nil, err
	}
	if !deduplicated {
		return uc.deliver(ctx, in)
	}
	return uc.deliverOnce(ctx, in, request)
}

func (uc *operatorSendUseCase) deliverOnce(ctx context.Context, in conversation.OperatorSendInput, request conversation.SendRequest) (*conversation.Message, error) {
	sent, err := uc.requests.Sent(request)
	if err == nil {
		return sent, nil
	}
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		return nil, err
	}

	claimed, err := uc.requests.Claim(request)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, conversation.ErrSendRequestInFlight
	}

	in.RequestID = request.RequestID
	message, err := uc.deliver(ctx, in)
	if err != nil {
		uc.requests.Release(request)
		return nil, err
	}
	return message, nil
}

func (uc *operatorSendUseCase) deliver(ctx context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	if in.WorkspaceID != "" {
		if err := uc.billing.AllowSend(in.WorkspaceID); err != nil {
			return nil, err
		}
	}

	message, err := uc.send(in)
	if err != nil {
		return nil, err
	}

	if err := uc.finalizer.FinalizeOperatorSend(ctx, conversation.FinalizeOperatorSendInput{
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		WorkspaceID: in.WorkspaceID,
		ActorUserID: in.SenderUserID,
		Message:     message,
	}); err != nil {
		log.Printf("[OperatorSend] finalize failed for %s (%s): %v", in.EntryID, in.EntryType, err)
	}

	return message, nil
}

func (uc *operatorSendUseCase) send(in conversation.OperatorSendInput) (*conversation.Message, error) {
	origin := conversation.SendOrigin{
		SenderUserID:     in.SenderUserID,
		ReplyToMessageID: in.ReplyToMessageID,
		ClientRequestID:  in.RequestID,
	}
	if in.Buttons != nil {
		return uc.sender.SendButtonMessage(in.EntryID, in.EntryType, *in.Buttons, origin)
	}

	text := in.Text
	if in.Signed {
		text = conversation.SignOutbound(shared.EntryType(in.EntryType), uc.senderName(in.SenderUserID), text)
	}

	if in.MediaID != "" {
		return uc.sender.SendMediaMessage(in.EntryID, in.EntryType, in.MediaID, in.MediaType, text, origin)
	}
	return uc.sender.SendTextMessage(in.EntryID, in.EntryType, text, origin)
}

func (uc *operatorSendUseCase) senderName(userID string) string {
	record, err := uc.users.FindByID(userID)
	if err != nil || record == nil {
		log.Printf("[OperatorSend] could not resolve sender %s for the signature: %v", userID, err)
		return ""
	}
	return record.Username
}

var _ conversation.OperatorSendUseCase = (*operatorSendUseCase)(nil)
