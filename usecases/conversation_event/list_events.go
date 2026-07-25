package conversation_event_usecase

import ce "vozko/domain/conversation_event"

type listEventsUseCase struct {
	repo ce.Repository
}

func NewListEventsUseCase(repo ce.Repository) ce.ListEventsUseCase {
	return &listEventsUseCase{repo: repo}
}

func (uc *listEventsUseCase) Execute(workspaceID, entryID, entryType string, limit, offset int) ([]*ce.ConversationEvent, int64, error) {
	return uc.repo.ListByEntry(workspaceID, entryID, entryType, limit, offset)
}
