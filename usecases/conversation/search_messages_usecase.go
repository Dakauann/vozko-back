package conversation_usecase

import "vozko/domain/conversation"

type searchMessagesByEntryUseCase struct {
	messageRepo conversation.MessageRepository
}

func NewSearchMessagesByEntryUseCase(messageRepo conversation.MessageRepository) conversation.SearchMessagesByEntryUseCase {
	return &searchMessagesByEntryUseCase{messageRepo: messageRepo}
}

func (uc *searchMessagesByEntryUseCase) Execute(input conversation.SearchMessagesByEntryInput) ([]*conversation.Message, int64, error) {
	return uc.messageRepo.SearchMessagesByEntry(input)
}
