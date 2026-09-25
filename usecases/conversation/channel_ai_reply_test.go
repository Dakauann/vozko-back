package conversation_usecase

import (
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

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

		{"conversation overridden off", func(r *conversation.AIReplyRequest) { r.AutomationEnabled = &disabled }, false},

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

	if got[len(got)-1].Role != ai.RoleUser {
		t.Errorf("the final turn must be the customer's, got role %q", got[len(got)-1].Role)
	}
}

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

type stubAgentRepo struct{ agent.Repository }

type stubAIService struct{ ai.Service }

type stubMessageRepo struct {
	conversation.MessageRepository
	newestFirst []*conversation.Message
}

func (r stubMessageRepo) ListByEntryPaginated(conversation.ListMessagesInput) ([]*conversation.Message, error) {
	return r.newestFirst, nil
}

func TestBuildPromptGivesTheCustomerRoleOnlyToTheContact(t *testing.T) {
	svc := &ChannelAIReplyService{messages: stubMessageRepo{newestFirst: []*conversation.Message{
		{Text: "enviado do celular", MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentExternally()},
		{Text: "catalogo.pdf", MessageType: conversation.MessageTypeMedia, SentBy: conversation.SentByWorkflow("wf-1")},
		{Text: "posso ajudar", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("user-1")},
		{Text: "oi", MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511")},
	}}}

	got, err := svc.buildPrompt(conversation.AIReplyRequest{EntryID: "conv-1", EntryType: shared.EntryTypeUnofficialWhatsApp}, "oi de novo")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	want := []ai.Role{ai.RoleUser, ai.RoleAssistant, ai.RoleAssistant, ai.RoleAssistant, ai.RoleUser}
	if len(got) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(got), len(want), got)
	}
	for i, role := range want {
		if got[i].Role != role {
			t.Errorf("turn %d %q is %s, want %s", i, got[i].Content, got[i].Role, role)
		}
	}
}
