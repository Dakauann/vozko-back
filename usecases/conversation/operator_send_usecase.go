package conversation_usecase

import (
	"context"
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
}

func NewOperatorSendUseCase(
	sender conversation.MessageSender,
	users user.UserRepository,
	finalizer conversation.OperatorSendFinalizer,
	billing conversation.ServiceMessageBilling,
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
	if len(missing) > 0 {
		return nil, fmt.Errorf("operator send use case: missing %s", strings.Join(missing, ", "))
	}

	return &operatorSendUseCase{sender: sender, users: users, finalizer: finalizer, billing: billing}, nil
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
	if in.Buttons != nil {
		return uc.sender.SendButtonMessage(in.EntryID, in.EntryType, in.SenderUserID, in.ReplyToMessageID, *in.Buttons)
	}

	text := in.Text
	if in.Signed {
		text = conversation.SignOutbound(shared.EntryType(in.EntryType), uc.senderName(in.SenderUserID), text)
	}

	if in.MediaID != "" {
		return uc.sender.SendMediaMessage(
			in.EntryID, in.EntryType, in.MediaID, in.MediaType,
			in.SenderUserID, in.ReplyToMessageID, text,
		)
	}
	return uc.sender.SendTextMessage(in.EntryID, in.EntryType, text, in.SenderUserID, in.ReplyToMessageID)
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
