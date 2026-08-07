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

// The transcript must reach the model oldest-first. The repository returns it
// newest-first, because that is what makes the LIMIT take the most recent
// window, so replaying the page as returned fed the model the conversation
// backwards: the turn immediately before the one being answered was the OLDEST
// message, normally "/start". The agent then opened with its greeting on every
// single message and looked like it had no memory, while WhatsApp, which loads
// history ASC, behaved correctly on the identical agent.
func TestBuildPromptReplaysHistoryOldestFirst(t *testing.T) {
	svc := &ChannelAIReplyService{messages: stubMessageRepo{newestFirst: []*conversation.Message{
		{Text: "?", MessageType: conversation.MessageTypeUserMessage},
		{Text: "Olá! Eu sou um assistente virtual.", MessageType: conversation.MessageTypeAIResponse},
		{Text: "Quero falar sobre o atual governo", MessageType: conversation.MessageTypeUserMessage},
		{Text: "Olá! Como posso ajudar você hoje?", MessageType: conversation.MessageTypeAIResponse},
		{Text: "Oi", MessageType: conversation.MessageTypeUserMessage},
		{Text: "/start", MessageType: conversation.MessageTypeUserMessage},
	}}}

	got, err := svc.buildPrompt(conversation.AIReplyRequest{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}, "?")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	wantText := []string{
		"/start",
		"Oi",
		"Olá! Como posso ajudar você hoje?",
		"Quero falar sobre o atual governo",
		"Olá! Eu sou um assistente virtual.",
		"?",
	}
	if len(got) != len(wantText) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(wantText), got)
	}
	for i, want := range wantText {
		if got[i].Content != want {
			t.Errorf("message %d = %q, want %q", i, got[i].Content, want)
		}
	}

	// The newest message is already the last turn, so it must not be appended a
	// second time. Reversed, the comparison hit the oldest message and always
	// duplicated it.
	if got[len(got)-1].Role != ai.RoleUser {
		t.Errorf("the final turn must be the customer's, got role %q", got[len(got)-1].Role)
	}
}

// Tool bookkeeping and call events are transcript rows, not dialogue. WhatsApp
// filters them out of the prompt; this path must agree, or the model reads a
// tool result as something a human said.
func TestBuildPromptSkipsNonDialogueRows(t *testing.T) {
	svc := &ChannelAIReplyService{messages: stubMessageRepo{newestFirst: []*conversation.Message{
		{Text: "e o preço?", MessageType: conversation.MessageTypeUserMessage},
		{Text: "{\"price\": 120}", MessageType: conversation.MessageTypeToolResult},
		{Text: "consultar_preco", MessageType: conversation.MessageTypeToolCall},
		{Text: "  ", MessageType: conversation.MessageTypeUserMessage},
		{Text: "bom dia", MessageType: conversation.MessageTypeUserMessage},
	}}}

	got, err := svc.buildPrompt(conversation.AIReplyRequest{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}, "e o preço?")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	want := []string{"bom dia", "e o preço?"}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("message %d = %q, want %q", i, got[i].Content, w)
		}
	}
}

// --- minimal stubs: the gate must not touch these, so they only need to exist ---

type stubAgentRepo struct{ agent.Repository }

type stubAIService struct{ ai.Service }

// stubMessageRepo mimics ListByEntryPaginated's newest-first contract.
type stubMessageRepo struct {
	conversation.MessageRepository
	newestFirst []*conversation.Message
}

func (r stubMessageRepo) ListByEntryPaginated(conversation.ListMessagesInput) ([]*conversation.Message, error) {
	return r.newestFirst, nil
}
