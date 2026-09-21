package conversation_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
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

	if !strings.Contains(in.SystemPrompt, "Entregamos no Nordeste") {
		t.Errorf("the knowledge base was not injected: %q", in.SystemPrompt)
	}
	if strings.Index(in.SystemPrompt, "Você é a Bia.") > strings.Index(in.SystemPrompt, "Entregamos no Nordeste") {
		t.Error("grounding must follow the agent prompt")
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
	if len(in.Messages) != 1 {
		t.Errorf("messages = %+v", in.Messages)
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
