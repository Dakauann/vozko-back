package conversation_usecase

import (
	"context"
	"fmt"
	"log"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type ConversationAutomationService struct {
	setters     map[shared.EntryType]AutomationSetter
	hub         conversation.EventBroadcaster
	delegations conversation.DelegationRepository
}

type AutomationSetter func(ctx context.Context, entryID string, enabled *bool) error

func NewConversationAutomationService(hub conversation.EventBroadcaster) *ConversationAutomationService {
	return &ConversationAutomationService{
		setters: make(map[shared.EntryType]AutomationSetter),
		hub:     hub,
	}
}

func (s *ConversationAutomationService) WithDelegations(delegations conversation.DelegationRepository) *ConversationAutomationService {
	s.delegations = delegations
	return s
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

	if pauses(enabled) && s.delegations != nil {
		if err := s.delegations.Delete(ctx, entryID, entryType); err != nil {
			return fmt.Errorf("automation paused for %s (%s) but its delegation still overrides it: %w", entryID, entryType, err)
		}
	}

	if s.hub != nil {
		go s.hub.BroadcastEntryUpdate(entryID, string(entryType), nil)
	}
	return nil
}

func pauses(enabled *bool) bool {
	return enabled != nil && !*enabled
}
