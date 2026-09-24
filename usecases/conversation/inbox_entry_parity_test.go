package conversation_usecase

import (
	"testing"

	"vozko/domain/actor"
	"vozko/domain/agent"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type stubEntryWorkspaces map[string]string

func (s stubEntryWorkspaces) GetEntryWorkspaceID(entryID, _ string) (string, error) {
	return s[entryID], nil
}

type stubLeadsByID struct {
	lead.Repository
	workspaceID string
	leads       []*lead.Lead
}

func (s stubLeadsByID) FindByIDs(workspaceID string, _ []string) ([]*lead.Lead, error) {
	if workspaceID != s.workspaceID {
		return nil, nil
	}
	return s.leads, nil
}

type stubAssignmentsByEntry struct {
	ia.Repository
	workspaceID string
	rows        []*ia.InboxAssignment
}

func (s stubAssignmentsByEntry) FindByEntries(workspaceID string, _ []string) ([]*ia.InboxAssignment, error) {
	if workspaceID != s.workspaceID {
		return nil, nil
	}
	return s.rows, nil
}

type stubAgentsByID struct {
	agent.Repository
	agents []*agent.Agent
}

func (s stubAgentsByID) FindByIDs([]string) ([]*agent.Agent, error) { return s.agents, nil }

// A live update rebuilds the card from GetInboxEntry and the screen swaps it in
// whole. Built without the workspace on channels other than official WhatsApp,
// it dropped the contact, the assignee and the AI chip the list had shown.
func TestInboxEntryMatchesTheListOnEveryChannel(t *testing.T) {
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
	} {
		t.Run(string(entryType), func(t *testing.T) {
			svc := &HistoryProviderService{
				messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
					EntryID: "entry-1", EntryType: entryType, LeadID: "lead-1",
					CampaignID: "camp-1", CampaignName: "Teste nova roleta",
					LastMessageType: conversation.MessageTypeOperator,
					AgentID:         "agent-1", AgentResponsesEnabled: true,
				}},
				leadRepo: stubLeadsByID{workspaceID: "ws-1", leads: []*lead.Lead{{ID: "lead-1", Name: "DakauannT", Number: "5511999"}}},
				assignmentRepo: stubAssignmentsByEntry{workspaceID: "ws-1", rows: []*ia.InboxAssignment{
					{EntryID: "entry-1", AssignedUserID: actor.FormatAI("agent-1")},
				}},
				agentRepo: stubAgentsByID{agents: []*agent.Agent{{ID: "agent-1", Name: "Prospector de Dentistas"}}},
			}
			svc.SetEntryWorkspaces(stubEntryWorkspaces{"entry-1": "ws-1"})

			entry, err := svc.GetInboxEntry("entry-1", string(entryType))
			if err != nil {
				t.Fatalf("GetInboxEntry: %v", err)
			}
			if entry.LeadName != "DakauannT" || entry.LeadNumber != "5511999" {
				t.Errorf("contact = %q/%q, want the lead the list shows", entry.LeadName, entry.LeadNumber)
			}
			if entry.AssignedUserID != actor.FormatAI("agent-1") || entry.AssignedUsername != "Prospector de Dentistas" {
				t.Errorf("assignee = %q (%q), want the agent holding it", entry.AssignedUserID, entry.AssignedUsername)
			}
			if entry.AIHandler == nil || entry.AIHandler.AgentName != "Prospector de Dentistas" {
				t.Errorf("AI chip = %+v, want the channel's agent", entry.AIHandler)
			}
			if entry.CampaignName != "Teste nova roleta" {
				t.Errorf("campaign = %q, want Teste nova roleta", entry.CampaignName)
			}
		})
	}
}

// A finished conversation shows who closed it and with which outcome, on every
// channel and in both the list and the live update.
func TestInboxEntryCarriesHowItWasClosedOnEveryChannel(t *testing.T) {
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
	} {
		t.Run(string(entryType), func(t *testing.T) {
			row := conversation.EntryWithLastMessage{
				EntryID: "entry-1", EntryType: entryType,
				LastMessageType:    conversation.MessageTypeOperator,
				ConversationStatus: string(conversation.ConversationStatusFinished),
				Close: conversation.CloseRecord{
					Source: conversation.CloseSourceAI, Reason: conversation.CloseReasonAIResolved, Outcome: "sale",
				},
			}
			svc := &HistoryProviderService{messageRepo: stubEntryLastMessage{entry: row}}

			live, err := svc.GetInboxEntry("entry-1", string(entryType))
			if err != nil {
				t.Fatalf("GetInboxEntry: %v", err)
			}
			listed := svc.buildInboxEntries([]conversation.EntryWithLastMessage{row}, "")[0]

			for name, entry := range map[string]conversation.InboxEntry{"live": *live, "list": listed} {
				if entry.CloseSource != conversation.CloseSourceAI || entry.CloseReason != conversation.CloseReasonAIResolved || entry.CloseOutcome != "sale" {
					t.Errorf("%s: closed by %q (%q) with %q, want ai (ai_resolved) with sale", name, entry.CloseSource, entry.CloseReason, entry.CloseOutcome)
				}
			}
		})
	}
}
