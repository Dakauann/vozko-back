package carriermessaging_usecase

import (
	"context"
	"log"

	"vozko/domain/carriermessaging"
	conversation_usecase "vozko/usecases/conversation"
)

type AssignmentEnsurer interface {
	EnsureAssignment(entryID, entryType, senderID string) string
}

type AutomationDispatcher interface {
	Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error
}

type sharedInbox struct {
	assignments AssignmentEnsurer
	automation  AutomationDispatcher
}

func NewInbox(assignments AssignmentEnsurer, automation AutomationDispatcher) Inbox {
	return &sharedInbox{assignments: assignments, automation: automation}
}

func (i *sharedInbox) EnsureAssignment(_ context.Context, _ string, channel carriermessaging.Channel, conversationID, senderID string) error {
	i.assignments.EnsureAssignment(conversationID, string(channel.EntryType()), senderID)
	return nil
}

func (i *sharedInbox) DispatchAutomation(ctx context.Context, in AutomationInput) {
	err := i.automation.Dispatch(ctx, conversation_usecase.InboundAutomationInput{
		WorkspaceID: in.WorkspaceID, EntryID: in.ConversationID, EntryType: in.Channel.EntryType(),
		ContactRef: in.ContactRef, ProviderMessageID: in.ProviderMessageID, ReceivedAt: in.ReceivedAt,
		LeadID: in.LeadID, Text: in.Text, Selection: in.Selection,
		ConversationOverride: in.ConversationOverride, Config: in.Config,
	})
	if err != nil {
		log.Printf("[carrier] automation of conversation %s: %v", in.ConversationID, err)
	}
}
