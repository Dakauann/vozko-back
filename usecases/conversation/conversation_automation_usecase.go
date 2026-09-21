package conversation_usecase

import (
	"context"
	"log"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type ConversationAutomationService struct {
	setters map[shared.EntryType]AutomationSetter
	hub     conversation.EventBroadcaster
}

type AutomationSetter func(ctx context.Context, entryID string, enabled *bool) error

func NewConversationAutomationService(hub conversation.EventBroadcaster) *ConversationAutomationService {
	return &ConversationAutomationService{
		setters: make(map[shared.EntryType]AutomationSetter),
		hub:     hub,
	}
}

func (s *ConversationAutomationService) Register(entryType shared.EntryType, setter AutomationSetter) {
	if s == nil || setter == nil {
		return
	}
	s.setters[entryType] = setter
}

func (s *ConversationAutomationService) SetAutomation(
	ctx context.Context,
	entryID string,
	entryType shared.EntryType,
	enabled *bool,
) error {
	if s == nil {
		return conversation.ErrEntryTypeInvalid
	}
	if entryID == "" {
		return conversation.ErrConversationNotFound
	}

	setter, ok := s.setters[entryType]
	if !ok {
		log.Printf("[automation] no automation setter registered for %s", entryType)
		return conversation.ErrEntryTypeInvalid
	}

	if err := setter(ctx, entryID, enabled); err != nil {
		return err
	}

	if s.hub != nil {
		go s.hub.BroadcastEntryUpdate(entryID, string(entryType), nil)
	}
	return nil
}
