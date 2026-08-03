package conversation_usecase

import (
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The automation gate decides whether a bot speaks to a customer. Getting it
// wrong is visible and embarrassing in both directions, a silent agent, or an
// agent talking over an operator who has taken the conversation, so every
// branch is pinned here.
//
// The contract mirrors WhatsApp's: the per-conversation override wins when set,
// otherwise the account-level switch decides.
func TestChannelAIReplyGate(t *testing.T) {
	enabled, disabled := true, false

	base := func() conversation.AIReplyRequest {
		return conversation.AIReplyRequest{
			WorkspaceID:           "ws-1",
			EntryID:               "conv-1",
			EntryType:             shared.EntryTypeInstagram,
			AgentID:               "agent-1",
			AgentResponsesEnabled: true,
			Text:                  "oi, vocês entregam em Recife?",
		}
	}

	cases := []struct {
		name    string
		mutate  func(*conversation.AIReplyRequest)
		wantRun bool
	}{
		{"account on, never overridden", func(r *conversation.AIReplyRequest) {}, true},
		{"account on, conversation explicitly on", func(r *conversation.AIReplyRequest) { r.AutomationEnabled = &enabled }, true},

		// An operator took this conversation over: the agent must go quiet here
		// even though the account-wide agent stays on for everyone else.
		{"conversation overridden off", func(r *conversation.AIReplyRequest) { r.AutomationEnabled = &disabled }, false},

		// The override also cannot switch an agent ON for an account that has
		// agent responses disabled, otherwise a stale per-conversation flag would
		// resurrect a bot the tenant turned off.
		{"account off, conversation on", func(r *conversation.AIReplyRequest) {
			r.AgentResponsesEnabled = false
			r.AutomationEnabled = &enabled
		}, false},

		{"account off", func(r *conversation.AIReplyRequest) { r.AgentResponsesEnabled = false }, false},
		{"no agent configured", func(r *conversation.AIReplyRequest) { r.AgentID = "" }, false},
		{"blank agent id", func(r *conversation.AIReplyRequest) { r.AgentID = "   " }, false},
	}

	svc := &ChannelAIReplyService{
		agents:    stubAgentRepo{},
		aiService: stubAIService{},
		sender:    &MessageSenderService{},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base()
			tc.mutate(&req)
			if got := svc.enabled(req); got != tc.wantRun {
				t.Errorf("enabled = %v, want %v", got, tc.wantRun)
			}
		})
	}
}

// A service missing any collaborator must stay silent rather than panic. This is
// the WhatsApp-only deployment: no channel adapters, no AI wiring.
func TestChannelAIReplyGateRequiresFullWiring(t *testing.T) {
	req := conversation.AIReplyRequest{
		AgentID:               "agent-1",
		AgentResponsesEnabled: true,
		Text:                  "hi",
	}

	for name, svc := range map[string]*ChannelAIReplyService{
		"no wiring at all": {},
		"no ai service":    {agents: stubAgentRepo{}, sender: &MessageSenderService{}},
		"no agent repo":    {aiService: stubAIService{}, sender: &MessageSenderService{}},
		"no sender":        {agents: stubAgentRepo{}, aiService: stubAIService{}},
	} {
		t.Run(name, func(t *testing.T) {
			if svc.enabled(req) {
				t.Error("an incompletely wired service must not attempt a reply")
			}
		})
	}
}

// --- minimal stubs: the gate must not touch these, so they only need to exist ---

type stubAgentRepo struct{ agent.Repository }

type stubAIService struct{ ai.Service }
