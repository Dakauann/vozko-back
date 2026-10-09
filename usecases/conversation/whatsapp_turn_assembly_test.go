package conversation_usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/rag"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/agentturn"
)

type waTurnRAG struct{ results []rag.QueryResult }

func (f waTurnRAG) Query(context.Context, rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}
func (f waTurnRAG) QueryForAgent(context.Context, rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}

func waTurnUseCase(results ...rag.QueryResult) *handleWhatsAppMessageUseCase {
	uc := &handleWhatsAppMessageUseCase{}
	uc.SetTurnAssembler(agentturn.New(nil, waTurnRAG{results: results}, nil))
	return uc
}

func waAgent() *agent.Agent {
	return &agent.Agent{
		ID:               "agent-1",
		WorkspaceID:      "ws-1",
		Name:             "Bia",
		MessagingPrompt:  "Você é a Bia.",
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb-1"},
	}
}

func waAgentCtx() *agentContext {
	return &agentContext{
		agent:      waAgent(),
		wcCampaign: &wc.Campaign{ID: "camp-1", Name: "Promo"},
		wcEntry:    &wce.WhatsAppCampaignEntry{ID: "entry-1"},
		tools: []ResolvedTool{{
			Definition: toolsdomain.Definition{Name: "manage_entry_stage"},
			Config:     map[string]interface{}{"pipeline_id": "pipe-1"},
		}},
	}
}

func waTurn() whatsAppTurn {
	return whatsAppTurn{
		agentCtx: waAgentCtx(),
		whatsappCtx: WhatsAppContext{
			UserPhoneNumber: "5511999999999",
			UserName:        "Ana",
			ConversationID:  "conv-1",
			AgentName:       "Bia",
		},
		RecipientPhone:  "5511999999999",
		BusinessPhoneID: "phone-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		Query:           "vocês entregam em Recife?",
		Messages:        []ai.Message{{Role: ai.RoleUser, Content: "vocês entregam em Recife?"}},
		Model:           "gpt-x",
		Temperature:     0.2,
		Segmented:       true,
	}
}

func TestWhatsAppTurnStampsEverySeedOntoEveryTool(t *testing.T) {
	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), waTurn())

	cfg := in.ToolConfigs["manage_entry_stage"]
	if cfg == nil {
		t.Fatalf("no tool config; keys = %v", in.ToolConfigs)
	}

	for key, want := range map[string]string{
		"__recipient_phone":   "5511999999999",
		"__business_phone_id": "phone-1",
		"__entry_id":          "entry-1",
		"__entry_type":        string(shared.EntryTypeWhatsApp),
		"__workspace_id":      "ws-1",
		"__campaign_id":       "camp-1",
		"__campaign_type":     "whatsapp",
	} {
		if got := cfg[key]; got != want {
			t.Errorf("%s = %v, want %q", key, got, want)
		}
	}

	if cfg["pipeline_id"] != "pipe-1" {
		t.Errorf("the resolved tool config was lost: %+v", cfg)
	}
}

func TestWhatsAppTurnDoesNotMutateTheResolvedToolSet(t *testing.T) {
	turn := waTurn()
	original := turn.agentCtx.tools[0].Config

	waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if _, leaked := original["__entry_id"]; leaked {
		t.Errorf("the shared resolved tool config was mutated: %+v", original)
	}
}

func TestWhatsAppTurnKeepsTheIdentityPreambleAndAgentPrompt(t *testing.T) {
	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), waTurn())

	if !strings.Contains(in.SystemPrompt, "CANAL: WHATSAPP") {
		t.Errorf("the WhatsApp identity preamble is missing: %q", in.SystemPrompt)
	}
	if !strings.Contains(in.SystemPrompt, "Você é a Bia.") {
		t.Errorf("the agent prompt is missing: %q", in.SystemPrompt)
	}
	if strings.Index(in.SystemPrompt, "CANAL: WHATSAPP") > strings.Index(in.SystemPrompt, "Você é a Bia.") {
		t.Error("the preamble must precede the agent prompt")
	}
}

func TestWhatsAppTurnTellsTheModelItHasTools(t *testing.T) {
	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), waTurn())

	if !strings.Contains(in.SystemPrompt, "USO DE FERRAMENTAS") {
		t.Errorf("the tool instruction is missing despite a tool being attached: %q", in.SystemPrompt)
	}
	if len(in.Tools) != 1 {
		t.Errorf("tools = %+v, want the pre-resolved tool", in.Tools)
	}
}

func TestWhatsAppTurnIsGroundedInTheKnowledgeBase(t *testing.T) {
	uc := waTurnUseCase(rag.QueryResult{DocumentName: "entregas", Content: "Entregamos no Nordeste.", Score: 0.9})

	in := uc.assembleWhatsAppTurn(context.Background(), waTurn())

	if !strings.Contains(contextNoteOf(in).Content, "Entregamos no Nordeste") {
		t.Errorf("the knowledge base was not injected into the context note: %q", contextNoteOf(in).Content)
	}
	if strings.Contains(in.SystemPrompt, "Entregamos no Nordeste") {
		t.Errorf("per-message grounding leaked into the system prompt: %q", in.SystemPrompt)
	}
	if got := in.Messages[len(in.Messages)-2]; got.Content != "vocês entregam em Recife?" {
		t.Errorf("the customer's message must precede the note untouched, got %+v", got)
	}
}

func TestWhatsAppTurnRoutesAndBillsUnderTheEntry(t *testing.T) {
	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), waTurn())

	if in.SessionID != "agent_reply:entry-1" || in.BillingReference != "agent_reply:entry-1" {
		t.Errorf("session = %q, billing = %q, want agent_reply:entry-1", in.SessionID, in.BillingReference)
	}
	if in.VolatileTail != 1 || !strings.HasPrefix(contextNoteOf(in).Content, ai.ContextNote().Content) {
		t.Errorf("the turn must end with a volatile context note: %+v (tail %d)", contextNoteOf(in), in.VolatileTail)
	}
}

func TestConsecutiveWhatsAppTurnsShareToolsAndSystemPrompt(t *testing.T) {
	uc := waTurnUseCase(rag.QueryResult{DocumentName: "entregas", Content: "Entregamos no Nordeste.", Score: 0.9})
	metadata := map[string]interface{}{
		"cidade": "Recife", "plano": "ouro", "origem": "anuncio", "bairro": "Boa Viagem",
		"cpf": "000", "estado": "PE", "zona": "sul", "idade": 31,
	}

	first := waTurn()
	first.whatsappCtx.Metadata = metadata
	second := waTurn()
	second.whatsappCtx.Metadata = metadata
	second.Query = "e o prazo?"
	second.Messages = []ai.Message{
		{Role: ai.RoleUser, Content: "vocês entregam em Recife?"},
		{Role: ai.RoleAssistant, Content: "Entregamos sim!"},
		{Role: ai.RoleUser, Content: "e o prazo?"},
	}

	for round := 0; round < 10; round++ {
		a := uc.assembleWhatsAppTurn(context.Background(), first)
		b := uc.assembleWhatsAppTurn(context.Background(), second)
		if a.SystemPrompt != b.SystemPrompt {
			t.Fatalf("round %d: system prompt changed between turns:\n%s\n---\n%s", round, a.SystemPrompt, b.SystemPrompt)
		}
		if len(a.Tools) != len(b.Tools) || a.Tools[0].Name != b.Tools[0].Name {
			t.Fatalf("round %d: tools changed between turns: %+v vs %+v", round, a.Tools, b.Tools)
		}
	}
}

func TestWhatsAppHistoryWindowHoldsItsStartAcrossTurns(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	oldest := map[string]bool{}
	for total := 190; total <= 195; total++ {
		history := make([]*conversation.Message, 0, total)
		for i := 0; i < total; i++ {
			history = append(history, &conversation.Message{Text: fmt.Sprintf("m%03d", i), From: "5511999999999", MessageType: conversation.MessageTypeUserMessage})
		}

		composed := uc.composeConversationHistory(history, "5511888888888", "5511999999999")

		if len(composed) > conversationHistoryLimit {
			t.Fatalf("total %d: window holds %d messages, want at most %d", total, len(composed), conversationHistoryLimit)
		}
		if composed[len(composed)-1].Content != fmt.Sprintf("m%03d", total-1) {
			t.Fatalf("total %d: window ends with %q", total, composed[len(composed)-1].Content)
		}
		oldest[composed[0].Content] = true
	}
	if len(oldest) != 1 {
		t.Fatalf("the window start moved between turns: %v", oldest)
	}
}

func TestWhatsAppTurnCarriesTheGenerationKnobs(t *testing.T) {
	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), waTurn())

	if in.Model != "gpt-x" {
		t.Errorf("Model = %q", in.Model)
	}
	if in.Temperature != 0.2 {
		t.Errorf("Temperature = %v", in.Temperature)
	}
	if !in.SegmentedResponse {
		t.Error("SegmentedResponse lost")
	}
	if in.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q", in.WorkspaceID)
	}
	if len(in.Messages) != 2 {
		t.Errorf("messages = %+v, want the customer and the context note", in.Messages)
	}
}

func TestWhatsAppTurnUsesTheCallersBusinessPhone(t *testing.T) {
	turn := waTurn()
	turn.BusinessPhoneID = "fallback-phone"

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if got := in.ToolConfigs["manage_entry_stage"]["__business_phone_id"]; got != "fallback-phone" {
		t.Errorf("__business_phone_id = %v", got)
	}
}

func TestWhatsAppTurnOmitsCampaignSeedsWithoutACampaign(t *testing.T) {
	turn := waTurn()
	turn.agentCtx.wcCampaign = nil

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	cfg := in.ToolConfigs["manage_entry_stage"]
	if _, present := cfg["__campaign_id"]; present {
		t.Errorf("a campaign seed appeared without a campaign: %+v", cfg)
	}
	if cfg["__entry_id"] != "entry-1" {
		t.Errorf("__entry_id lost: %+v", cfg)
	}
}

func TestWhatsAppTurnInterpolatesTheAgentPrompt(t *testing.T) {
	turn := waTurn()
	turn.agentCtx.agent.MessagingPrompt = "Você atende {{cidade}}."
	turn.Vars = map[string]string{"cidade": "Recife"}

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if !strings.Contains(in.SystemPrompt, "Recife") {
		t.Errorf("vars not interpolated: %q", in.SystemPrompt)
	}
}

func TestWhatsAppTurnSurvivesWithoutAnAgent(t *testing.T) {
	turn := waTurn()
	turn.agentCtx = nil

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if len(in.Tools) != 0 {
		t.Errorf("tools = %+v, want none", in.Tools)
	}
	if !strings.Contains(in.SystemPrompt, "CANAL: WHATSAPP") {
		t.Error("the identity preamble should still be built")
	}
}

func TestAMediaMessageReplaysExactlyAsItWasSentLive(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	stored := &conversation.Message{Text: "[Image] cardápio", From: "5511999999999", MessageType: conversation.MessageTypeMedia, Metadata: conversation.ExtractedTextMetadata("Pizza grande R$ 50")}
	replayed := uc.composeConversationHistory([]*conversation.Message{stored}, "5511888888888", "5511999999999")
	if len(replayed) != 1 || replayed[0].Content != conversation.PromptContent("[Image] cardápio", "Pizza grande R$ 50") {
		t.Fatalf("the next turn must see the media message byte for byte as this turn sent it, got %+v", replayed)
	}
}
