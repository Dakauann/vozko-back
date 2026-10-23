package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
)

type recordingAssignments struct{ calls [][3]string }

func (r *recordingAssignments) EnsureAssignment(entryID, entryType, senderID string) string {
	r.calls = append(r.calls, [3]string{entryID, entryType, senderID})
	return "user-1"
}

type recordingAutomation struct {
	got []conversation_usecase.InboundAutomationInput
	err error
}

func (r *recordingAutomation) Dispatch(_ context.Context, in conversation_usecase.InboundAutomationInput) error {
	r.got = append(r.got, in)
	return r.err
}

func TestInboxGlueHandsTheMessageToTheSharedAutomation(t *testing.T) {
	assignments, automation := &recordingAssignments{}, &recordingAutomation{}
	inbox := NewInbox(assignments, automation)
	agent, override := "agent-1", false
	lead := "lead-1"
	sel := &workflow.OptionSelection{ID: "a", Title: "Sim", Kind: SelectionKind}
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	if err := inbox.EnsureAssignment(context.Background(), "ws-1", carriermessaging.ChannelSMS, "conv-1", "route-1"); err != nil {
		t.Fatalf("EnsureAssignment = %v", err)
	}
	if assignments.calls[0] != [3]string{"conv-1", "sms", "route-1"} {
		t.Fatalf("assignment = %v", assignments.calls)
	}
	inbox.DispatchAutomation(context.Background(), AutomationInput{
		WorkspaceID: "ws-1", Channel: carriermessaging.ChannelRCS, ConversationID: "conv-2", ContactRef: "5511999990000",
		LeadID: &lead, ProviderMessageID: "in-1", Text: "Sim", Selection: sel, ReceivedAt: at,
		Config: conversation.ChannelAutomation{AgentID: &agent, EnableAgentResponses: true}, ConversationOverride: &override,
	})
	got := automation.got[0]
	if got.EntryID != "conv-2" || got.EntryType != carriermessaging.ChannelRCS.EntryType() || got.Selection != sel || got.Config.AgentID != &agent || got.ConversationOverride != &override || *got.LeadID != "lead-1" {
		t.Fatalf("dispatched = %+v", got)
	}
}

func TestInboxGlueLogsAFailedDispatchWithoutFailingTheWebhook(t *testing.T) {
	inbox := NewInbox(&recordingAssignments{}, &recordingAutomation{err: errors.New("down")})
	inbox.DispatchAutomation(context.Background(), AutomationInput{WorkspaceID: "ws-1", Channel: carriermessaging.ChannelRCS})
}
