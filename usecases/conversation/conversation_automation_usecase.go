package conversation_usecase

import (
	"context"
	"log"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// ConversationAutomationService flips the per-conversation automation override,
// the switch an operator uses to take a conversation over from the agent.
//
// It exists because the only way to set it was PATCH on a WhatsApp CAMPAIGN
// entry. Instagram and Telegram conversations have no campaign, so the button
// resolved no campaign id and returned without issuing a request: no error, no
// feedback, nothing written. The control looked functional and did nothing.
//
// Each channel registers its own setter, so a channel added later either
// registers one or is refused by name, it cannot silently no-op again.
type ConversationAutomationService struct {
	setters map[shared.EntryType]AutomationSetter
	hub     conversation.EventBroadcaster
}

// AutomationSetter writes the override for one channel.
//
// nil means "clear the override", which restores inheritance from the account
// or campaign switch. That is a distinct state from an explicit false, and the
// inbound handlers read it that way, so the pointer is passed through rather
// than flattened to a bool.
type AutomationSetter func(ctx context.Context, entryID string, enabled *bool) error

func NewConversationAutomationService(hub conversation.EventBroadcaster) *ConversationAutomationService {
	return &ConversationAutomationService{
		setters: make(map[shared.EntryType]AutomationSetter),
		hub:     hub,
	}
}

// Register wires one channel's setter.
func (s *ConversationAutomationService) Register(entryType shared.EntryType, setter AutomationSetter) {
	if s == nil || setter == nil {
		return
	}
	s.setters[entryType] = setter
}

// SetAutomation applies the override and tells every viewer.
//
// The broadcast carries the entry's OWN type rather than a hardcoded channel:
// the WhatsApp-scoped handler this replaces always broadcast "whatsapp", so a
// toggle on any other channel would not have refreshed its own conversation
// even if the write had happened.
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
		// Named plainly. A channel with no setter is a wiring gap, and reporting
		// it as a missing conversation would send an operator looking for the
		// wrong thing.
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
