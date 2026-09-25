package conversation_usecase

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type personSend struct {
	access shared.EntryAccessChecker
	send   conversation.OperatorSendUseCase
}

func NewPersonSend(access shared.EntryAccessChecker, send conversation.OperatorSendUseCase) conversation.PersonSendUseCase {
	return &personSend{access: access, send: send}
}

func (uc *personSend) Execute(ctx context.Context, by shared.Person, in conversation.OperatorSendInput) (*conversation.Message, error) {
	if !shared.EntryType(in.EntryType).SupportsConversationView() {
		return nil, conversation.ErrEntryTypeInvalid
	}
	if !by.MayActOn(uc.access, in.WorkspaceID, in.EntryID, in.EntryType) {
		return nil, conversation.ErrUnauthorized
	}
	in.SenderUserID = by.UserID
	return uc.send.Execute(ctx, in)
}
