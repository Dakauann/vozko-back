package conversation_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type personTemplateSend struct {
	access shared.EntryAccessChecker
	sender conversation.TemplateSender
}

func NewPersonTemplateSend(access shared.EntryAccessChecker, sender conversation.TemplateSender) conversation.PersonTemplateSendUseCase {
	return &personTemplateSend{access: access, sender: sender}
}

func (uc *personTemplateSend) Execute(by shared.Person, in conversation.TemplateSendRequest) (string, error) {
	if !shared.EntryType(in.EntryType).SupportsConversationView() {
		return "", conversation.ErrEntryTypeInvalid
	}
	if !by.MayActOn(uc.access, in.WorkspaceID, in.EntryID, in.EntryType) {
		return "", conversation.ErrUnauthorized
	}
	return uc.sender.SendTemplate(in.EntryID, in.EntryType, in.TemplateID, in.Variables, by.UserID, in.WorkspaceID)
}
