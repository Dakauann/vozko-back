package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/user"
	"vozko/domain/workflow"
)

type ownerAssignmentsStub struct {
	ia.Repository
	rows []*ia.InboxAssignment
}

func (s *ownerAssignmentsStub) FindByEntries(string, []string) ([]*ia.InboxAssignment, error) {
	return s.rows, nil
}

type ownerUsersStub struct {
	user.UserRepository
	asked []string
}

func (s *ownerUsersStub) FindByIDs(ids []string) ([]*user.User, error) {
	s.asked = append(s.asked, ids...)
	return []*user.User{{ID: "user-1", Username: "ana"}}, nil
}

type ownerAgentsStub struct {
	agent.Repository
	asked []string
	err   error
}

func (s *ownerAgentsStub) FindByIDs(ids []string) ([]*agent.Agent, error) {
	s.asked = append(s.asked, ids...)
	if s.err != nil {
		return nil, s.err
	}
	return []*agent.Agent{{ID: "agent-1", Name: "Sofia"}}, nil
}

type ownerWorkflowsStub struct{}

func (ownerWorkflowsStub) FindByIDs([]string) ([]*workflow.Workflow, error) {
	return []*workflow.Workflow{{ID: "wf-1", Name: "Triagem"}}, nil
}

func ownerFixture(agents *ownerAgentsStub, users *ownerUsersStub) *HistoryProviderService {
	return &HistoryProviderService{
		assignmentRepo: &ownerAssignmentsStub{rows: []*ia.InboxAssignment{
			{EntryID: "e-human", AssignedUserID: "user-1"},
			{EntryID: "e-agent", AssignedUserID: "ai:agent-1"},
			{EntryID: "e-workflow", AssignedUserID: "workflow:wf-1"},
		}},
		userRepo:     users,
		agentRepo:    agents,
		workflowRepo: ownerWorkflowsStub{},
	}
}

func TestInboxNamesTheAutomationThatHoldsAConversation(t *testing.T) {
	// The web prints assigned_username as text in the row, the card and the
	// panel. An AI owner with no name there read as "Não atribuído".
	users := &ownerUsersStub{}
	agents := &ownerAgentsStub{}
	svc := ownerFixture(agents, users)

	entries := []conversation.InboxEntry{{EntryID: "e-human"}, {EntryID: "e-agent"}, {EntryID: "e-workflow"}}
	svc.enrichAssignments(entries, "ws-1")

	want := map[string][2]string{
		"e-human":    {"user-1", "ana"},
		"e-agent":    {"ai:agent-1", "Sofia"},
		"e-workflow": {"workflow:wf-1", "Triagem"},
	}
	for _, e := range entries {
		if got := [2]string{e.AssignedUserID, e.AssignedUsername}; got != want[e.EntryID] {
			t.Errorf("%s: owner = %v, want %v", e.EntryID, got, want[e.EntryID])
		}
	}
	for _, id := range users.asked {
		if id != "user-1" {
			t.Errorf("users were asked for %q: AI ids must never reach the users table", id)
		}
	}
	if len(agents.asked) != 1 || agents.asked[0] != "agent-1" {
		t.Errorf("agents were asked for %v, want only the bare agent-1: a workflow id is never an agent", agents.asked)
	}
}

func TestAnAgentLookupFailureStillNamesThePeople(t *testing.T) {
	svc := ownerFixture(&ownerAgentsStub{err: errors.New("db down")}, &ownerUsersStub{})

	entries := []conversation.InboxEntry{{EntryID: "e-human"}, {EntryID: "e-agent"}}
	svc.enrichAssignments(entries, "ws-1")

	if entries[0].AssignedUsername != "ana" {
		t.Errorf("human owner name = %q, want ana", entries[0].AssignedUsername)
	}
	if entries[1].AssignedUserID != "ai:agent-1" {
		t.Errorf("AI owner id = %q; the id must survive a failed name lookup", entries[1].AssignedUserID)
	}
}

func TestTheHandlerChipFollowsTheSharedGovernanceRule(t *testing.T) {
	cases := []struct {
		name     string
		row      conversation.EntryWithLastMessage
		run      *workflow.WorkflowRun
		wantKind string
		wantID   string
	}{
		{
			name:     "agent",
			row:      conversation.EntryWithLastMessage{AgentID: "agent-1", AgentResponsesEnabled: true},
			wantKind: "agent", wantID: "agent-1",
		},
		{
			name: "workflow over agent",
			row: conversation.EntryWithLastMessage{AgentID: "agent-1", AgentResponsesEnabled: true,
				WorkflowID: "wf-1", WorkflowEnabled: true},
			wantKind: "workflow", wantID: "wf-1",
		},
		{
			// The chip still names a paused AI ("IA pausada"); only ownership drops it.
			name:     "paused agent keeps its chip",
			row:      conversation.EntryWithLastMessage{AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: boolPtr(false)},
			wantKind: "agent", wantID: "agent-1",
		},
		{
			name:     "a running workflow shows even when none is configured",
			row:      conversation.EntryWithLastMessage{},
			run:      &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-9"},
			wantKind: "workflow", wantID: "wf-9",
		},
		{
			name: "nothing configured",
			row:  conversation.EntryWithLastMessage{AgentID: "agent-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := buildAIHandler(tc.row, tc.run, map[string]*agent.Agent{}, map[string]*workflow.Workflow{})
			if tc.wantKind == "" {
				if h != nil {
					t.Fatalf("handler = %+v, want none", h)
				}
				return
			}
			if h == nil || h.Kind != tc.wantKind {
				t.Fatalf("handler = %+v, want kind %q", h, tc.wantKind)
			}
			if id := h.AgentID + h.WorkflowID; id != tc.wantID {
				t.Fatalf("handler id = %q, want %q", id, tc.wantID)
			}
		})
	}
}
