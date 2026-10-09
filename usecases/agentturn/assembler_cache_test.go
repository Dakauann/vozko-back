package agentturn

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/rag"
	"vozko/domain/tools"
	shared_usecase "vozko/usecases/shared"
)

type echoRAG struct{}

func (echoRAG) Query(_ context.Context, in rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: []rag.QueryResult{{DocumentName: "faq", Content: "TRECHO sobre " + in.Query, Score: 0.9}}}, nil
}

func (echoRAG) QueryForAgent(_ context.Context, in rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: []rag.QueryResult{{DocumentName: "faq", Content: "TRECHO sobre " + in.Query, Score: 0.9}}}, nil
}

const leadHeader = "--- Lead/Contact Info"

func cachedAgent() *agent.Agent {
	return &agent.Agent{
		ID:               "agent-1",
		WorkspaceID:      "ws-1",
		Name:             "Bia",
		MessagingPrompt:  "Você é a Bia.",
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb-1"},
		InternalTools:    []agent.ToolBinding{{Name: "manage_entry_stage"}, {Name: "book_meeting"}},
	}
}

func leadIdentity(phone, name, conversationID string, metadata map[string]interface{}) *shared_usecase.ConversationContext {
	return &shared_usecase.ConversationContext{
		Channel:         shared_usecase.ChannelWhatsApp,
		UserPhoneNumber: phone,
		UserName:        name,
		ConversationID:  conversationID,
		CampaignName:    "Promo",
		Metadata:        metadata,
	}
}

func anaMetadata() map[string]interface{} {
	return map[string]interface{}{
		"cidade": "Recife", "plano": "ouro", "origem": "anuncio", "bairro": "Boa Viagem",
		"cpf": "000", "estado": "PE", "zona": "sul", "idade": 31,
	}
}

func cachedAssembler(memories leadmemory.ListUseCase) *Assembler {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}, {Name: "manage_entry_stage"}}}
	return New(reg, echoRAG{}, memories)
}

func turnRequest(identity *shared_usecase.ConversationContext, history []ai.Message, customerMessage string) Request {
	return Request{
		Agent:                cachedAgent(),
		Identity:             identity,
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__entry_id": "entry-1"},
		RAGQuery:             customerMessage,
		LeadID:               "lead-1",
		History:              history,
		UserMessage:          customerMessage,
		Session:              ReplySession("entry-1"),
	}
}

func TestConsecutiveTurnsOfAConversationShareToolsAndSystemPrompt(t *testing.T) {
	a := cachedAssembler(stubMemoryList{items: []leadmemory.MemoryView{memoryItem("Prefere boleto.")}})
	identity := leadIdentity("5511999990001", "Rosalinda", "conv-ana", anaMetadata())

	first := a.Assemble(context.Background(), turnRequest(identity, nil, "vocês entregam em Recife?"))
	second := a.Assemble(context.Background(), turnRequest(identity, []ai.Message{
		{Role: ai.RoleUser, Content: "vocês entregam em Recife?"},
		{Role: ai.RoleAssistant, Content: "Entregamos sim!"},
	}, "e qual o prazo?"))

	if first.Input.SystemPrompt != second.Input.SystemPrompt {
		t.Fatalf("system prompt changed between turns:\n%s\n---\n%s", first.Input.SystemPrompt, second.Input.SystemPrompt)
	}
	if strings.Join(first.ToolNames, ",") != strings.Join(second.ToolNames, ",") {
		t.Fatalf("tools changed between turns: %v vs %v", first.ToolNames, second.ToolNames)
	}
	for i := range first.Input.Tools {
		if first.Input.Tools[i].Name != second.Input.Tools[i].Name {
			t.Fatalf("tool %d changed between turns: %q vs %q", i, first.Input.Tools[i].Name, second.Input.Tools[i].Name)
		}
	}
}

func TestPerMessageGroundingRidesOnlyInTheTrailingContextNote(t *testing.T) {
	a := cachedAssembler(nil)
	history := []ai.Message{{Role: ai.RoleUser, Content: "oi"}, {Role: ai.RoleAssistant, Content: "olá!"}}

	out := a.Assemble(context.Background(), turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), history, "qual o preço?"))

	if strings.Contains(out.Input.SystemPrompt, "TRECHO sobre") {
		t.Fatalf("per-message grounding leaked into the system prompt:\n%s", out.Input.SystemPrompt)
	}
	messages := out.Input.Messages
	last := messages[len(messages)-1]
	if !strings.Contains(last.Content, "TRECHO sobre qual o preço?") {
		t.Fatalf("the trailing note does not carry the grounding: %q", last.Content)
	}
	for _, m := range messages[:len(messages)-1] {
		if strings.Contains(m.Content, "TRECHO sobre") {
			t.Fatalf("grounding leaked into a conversation message: %q", m.Content)
		}
	}
}

func TestTheLastMessageIsAContextNoteDeclaredVolatile(t *testing.T) {
	a := cachedAssembler(nil)

	out := a.Assemble(context.Background(), turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), nil, "qual o preço?"))

	last := out.Input.Messages[len(out.Input.Messages)-1]
	if last.Role != ai.RoleUser || !strings.HasPrefix(last.Content, ai.ContextNote().Content) {
		t.Fatalf("last message is not a context note: %+v", last)
	}
	if out.Input.VolatileTail != 1 {
		t.Fatalf("VolatileTail = %d, want 1", out.Input.VolatileTail)
	}
}

func TestAnUngroundedTurnStillEndsWithAContextNote(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:       &agent.Agent{MessagingPrompt: "hi"},
		History:     []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		UserMessage: "tudo bem?",
	})

	messages := out.Input.Messages
	if len(messages) != 3 {
		t.Fatalf("messages = %+v, want history, the customer and the note", messages)
	}
	if note := ai.ContextNote(); messages[2].Role != note.Role || messages[2].Content != note.Content {
		t.Fatalf("last message = %+v, want a bare context note", messages[2])
	}
	if out.Input.VolatileTail != 1 {
		t.Fatalf("VolatileTail = %d, want 1", out.Input.VolatileTail)
	}
}

func TestTheCustomersMessageStaysExactlyAsStored(t *testing.T) {
	a := cachedAssembler(nil)
	customer := "  vocês entregam em Recife?\n"

	out := a.Assemble(context.Background(), turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), nil, customer))

	messages := out.Input.Messages
	if got := messages[len(messages)-2]; got.Role != ai.RoleUser || got.Content != customer {
		t.Fatalf("customer message = %+v, want it untouched", got)
	}
}

func TestAConversationWithNoMessagesGetsNoNote(t *testing.T) {
	out := New(nil, nil, nil).Assemble(context.Background(), Request{Agent: &agent.Agent{MessagingPrompt: "hi"}})

	if len(out.Input.Messages) != 0 || out.Input.VolatileTail != 0 {
		t.Fatalf("messages = %+v, tail = %d, want neither", out.Input.Messages, out.Input.VolatileTail)
	}
}

func TestConversationsOfTheSameAgentShareThePromptUpToTheLeadBlock(t *testing.T) {
	a := cachedAssembler(nil)

	ana := a.Assemble(context.Background(), turnRequest(leadIdentity("5511999990001", "Rosalinda", "conv-ana", anaMetadata()), nil, "oi"))
	bruno := a.Assemble(context.Background(), turnRequest(leadIdentity("5511999990002", "Bruno", "conv-bruno", map[string]interface{}{"cidade": "Natal"}), nil, "oi"))

	anaAt := strings.Index(ana.Input.SystemPrompt, leadHeader)
	brunoAt := strings.Index(bruno.Input.SystemPrompt, leadHeader)
	if anaAt < 0 || anaAt != brunoAt {
		t.Fatalf("lead block at %d and %d, want the same offset:\n%s", anaAt, brunoAt, ana.Input.SystemPrompt)
	}
	if ana.Input.SystemPrompt[:anaAt] != bruno.Input.SystemPrompt[:brunoAt] {
		t.Fatalf("the shared prefix differs:\n%s\n---\n%s", ana.Input.SystemPrompt[:anaAt], bruno.Input.SystemPrompt[:brunoAt])
	}
	if !strings.Contains(ana.Input.SystemPrompt[:anaAt], "Você é a Bia.") {
		t.Fatalf("the agent prompt is not in the shared prefix:\n%s", ana.Input.SystemPrompt)
	}
	for _, leadFact := range []string{"5511999990001", "Rosalinda", "conv-ana", "Recife"} {
		if strings.Contains(ana.Input.SystemPrompt[:anaAt], leadFact) {
			t.Errorf("the shared prefix carries the lead fact %q", leadFact)
		}
	}
}

func TestSystemPromptRunsFromRulesToAgentToLeadToMemories(t *testing.T) {
	a := cachedAssembler(stubMemoryList{items: []leadmemory.MemoryView{memoryItem("Prefere boleto.")}})
	req := turnRequest(leadIdentity("5511999990001", "Rosalinda", "conv-ana", anaMetadata()), nil, "oi")
	req.PromptSuffix = "\n\n[SUFFIX]"

	sp := a.Assemble(context.Background(), req).Input.SystemPrompt

	order := []string{"REGRAS DE IDENTIDADE", "CLASSIFICAÇÃO DO LEAD", "Você é a Bia.", leadHeader, "Lead WhatsApp Number: 5511999990001", "cidade: Recife", "Memórias sobre este lead", "[SUFFIX]"}
	previous := -1
	for _, section := range order {
		at := strings.Index(sp, section)
		if at < 0 {
			t.Fatalf("section %q is missing:\n%s", section, sp)
		}
		if at < previous {
			t.Fatalf("section %q is out of order:\n%s", section, sp)
		}
		previous = at
	}
}

func TestTheSessionStampsRoutingAndBilling(t *testing.T) {
	out := cachedAssembler(nil).Assemble(context.Background(), turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), nil, "oi"))

	if out.Input.SessionID != "agent_reply:entry-1" {
		t.Errorf("SessionID = %q, want agent_reply:entry-1", out.Input.SessionID)
	}
	if out.Input.BillingReference != "agent_reply:entry-1" {
		t.Errorf("BillingReference = %q, want agent_reply:entry-1", out.Input.BillingReference)
	}
}

func TestSessionsAreNamedAfterTheFeatureAndTheSubject(t *testing.T) {
	if got := ReplySession("conv-1"); got != "agent_reply:conv-1" {
		t.Errorf("ReplySession = %q", got)
	}
	if got := SimulationSession("agent-1"); got != "agent_simulation:agent-1" {
		t.Errorf("SimulationSession = %q", got)
	}
	if got := ReplySession("  "); got != "" {
		t.Errorf("ReplySession without a subject = %q, want none", got)
	}
}
