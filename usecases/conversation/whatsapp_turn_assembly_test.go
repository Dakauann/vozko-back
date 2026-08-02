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

// WhatsApp's three agent turns — text, media and audio — each carried their own
// copy of the same assembly: interpolate the prompt, stamp seven seeds onto every
// tool, build the identity preamble from the resolved names, ground in the
// knowledge base. Three copies meant a fix landed in one and not the others.
//
// They now share the recipe. These pin what WhatsApp actually sends, because the
// migration cannot be verified against a live number.

type waTurnRAG struct{ results []rag.QueryResult }

func (f waTurnRAG) Query(context.Context, rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}
func (f waTurnRAG) QueryForAgent(context.Context, rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}

func waTurnUseCase(results ...rag.QueryResult) *handleWhatsAppMessageUseCase {
	uc := &handleWhatsAppMessageUseCase{}
	uc.SetTurnAssembler(agentturn.New(nil, waTurnRAG{results: results}))
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

// All seven seeds. A tool that loses any one of them acts on the wrong
// conversation, replies to the wrong number, or bills the wrong workspace.
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

	// The tool's own configured value must survive alongside the seeds.
	if cfg["pipeline_id"] != "pipe-1" {
		t.Errorf("the resolved tool config was lost: %+v", cfg)
	}
}

// Stamping must not write back into agentCtx.tools, which is resolved once per
// message and reused across the text, media and audio turns.
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
	// Ordering: preamble before the agent's own prompt, as it always was.
	if strings.Index(in.SystemPrompt, "CANAL: WHATSAPP") > strings.Index(in.SystemPrompt, "Você é a Bia.") {
		t.Error("the preamble must precede the agent prompt")
	}
}

// The preamble's tool instruction is gated on the turn actually carrying tools.
// Previously WhatsApp set AvailableTools by hand; the assembler now derives it,
// so this pins that the derivation still sees the pre-resolved set.
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
	// Grounding lands after the agent prompt, as it did inline.
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
	// The text turn is segmented; media and audio are not.
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

// The media turn prefers the campaign phone and falls back to the one the
// message arrived on; audio does the same. Both are expressed as the caller
// choosing BusinessPhoneID, so the seed simply reflects it.
func TestWhatsAppTurnUsesTheCallersBusinessPhone(t *testing.T) {
	turn := waTurn()
	turn.BusinessPhoneID = "fallback-phone"

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if got := in.ToolConfigs["manage_entry_stage"]["__business_phone_id"]; got != "fallback-phone" {
		t.Errorf("__business_phone_id = %v", got)
	}
}

// A conversation with no campaign must not invent one.
func TestWhatsAppTurnOmitsCampaignSeedsWithoutACampaign(t *testing.T) {
	turn := waTurn()
	turn.agentCtx.wcCampaign = nil

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	cfg := in.ToolConfigs["manage_entry_stage"]
	if _, present := cfg["__campaign_id"]; present {
		t.Errorf("a campaign seed appeared without a campaign: %+v", cfg)
	}
	// The conversation-scoped seeds still must be there.
	if cfg["__entry_id"] != "entry-1" {
		t.Errorf("__entry_id lost: %+v", cfg)
	}
}

// Vars come from different sources per turn (WhatsApp context metadata for text
// and audio, the campaign entry's for media), so interpolation is driven by the
// caller and must be applied.
func TestWhatsAppTurnInterpolatesTheAgentPrompt(t *testing.T) {
	turn := waTurn()
	turn.agentCtx.agent.MessagingPrompt = "Você atende {{cidade}}."
	turn.Vars = map[string]string{"cidade": "Recife"}

	in := waTurnUseCase().assembleWhatsAppTurn(context.Background(), turn)

	if !strings.Contains(in.SystemPrompt, "Recife") {
		t.Errorf("vars not interpolated: %q", in.SystemPrompt)
	}
}

// A turn with no agent must still produce a usable request rather than panic —
// the caller decides separately whether to skip the reply.
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
