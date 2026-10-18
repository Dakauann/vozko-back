package agentturn

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/tools"
	tools_usecase "vozko/usecases/tools"
)

type recentMessages struct {
	asked []conversation.ListMessagesInput
}

func (r *recentMessages) ListByEntryPaginated(in conversation.ListMessagesInput) ([]*conversation.Message, error) {
	r.asked = append(r.asked, in)
	return []*conversation.Message{
		{ID: "m-9", Text: "não quero mais", SentBy: conversation.SentByContact("5511"), CreatedAt: time.Now()},
	}, nil
}

func citingRegistry() stubRegistry {
	return stubRegistry{defs: []tools.Definition{{Name: tools_usecase.RecordOptOutToolName}, {Name: "manage_lead_memory"}}}
}

func lastNote(in ai.GenerateInput) string {
	return in.Messages[len(in.Messages)-1].Content
}

func agentWith(bindings ...agent.ToolBinding) *agent.Agent {
	return &agent.Agent{ID: "agent-1", WorkspaceID: "ws1", InternalTools: bindings}
}

func TestAgentsWithAConsentSkillSeeCitableMessageIDs(t *testing.T) {
	reader := &recentMessages{}
	a := New(citingRegistry(), nil, nil)
	a.CiteMessagesFrom(reader)

	out := a.Assemble(context.Background(), Request{
		Agent:                agentWith(agent.ToolBinding{Name: tools_usecase.RecordOptOutToolName}),
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		EntryID:              "entry-1",
		EntryType:            string(shared.EntryTypeInstagram),
		History:              []ai.Message{{Role: ai.RoleUser, Content: "não quero mais"}},
	})

	if note := lastNote(out.Input); !strings.Contains(note, "[m-9] lead: não quero mais") {
		t.Fatalf("the volatile context note must list citable ids:\n%s", note)
	}
	if strings.Contains(out.Input.SystemPrompt, "m-9") {
		t.Fatal("message ids change every turn and must stay out of the cached system prompt")
	}
	if len(reader.asked) != 1 || reader.asked[0].EntryID != "entry-1" || reader.asked[0].EntryType != shared.EntryTypeInstagram {
		t.Fatalf("asked %+v", reader.asked)
	}
}

func TestTheOfficialWhatsAppTurnCitesFromItsToolSeed(t *testing.T) {
	reader := &recentMessages{}
	a := New(citingRegistry(), nil, nil)
	a.CiteMessagesFrom(reader)

	out := a.Assemble(context.Background(), Request{
		Agent:       agentWith(),
		PreResolved: []tools.Definition{{Name: tools_usecase.RecordOptOutToolName}},
		ToolSeed:    map[string]interface{}{"__entry_id": "entry-7", "__entry_type": string(shared.EntryTypeWhatsApp)},
		History:     []ai.Message{{Role: ai.RoleUser, Content: "pare"}},
	})

	if !strings.Contains(lastNote(out.Input), "[m-9]") || len(reader.asked) != 1 || reader.asked[0].EntryID != "entry-7" {
		t.Fatalf("asked %+v, note:\n%s", reader.asked, lastNote(out.Input))
	}
}

func TestAgentsWithoutConsentSkillsGetNoMessageIDs(t *testing.T) {
	reader := &recentMessages{}
	a := New(citingRegistry(), nil, nil)
	a.CiteMessagesFrom(reader)

	out := a.Assemble(context.Background(), Request{
		Agent:                agentWith(agent.ToolBinding{Name: "manage_lead_memory"}),
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		EntryID:              "entry-1",
		EntryType:            string(shared.EntryTypeWhatsApp),
		History:              []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})

	if len(reader.asked) != 0 || strings.Contains(lastNote(out.Input), "Mensagens citáveis") {
		t.Fatalf("no consent skill, no lookup: asked %+v", reader.asked)
	}
}

func TestAConsentSkillWithoutAMessageReaderSaysThereAreNoIDs(t *testing.T) {
	out := New(citingRegistry(), nil, nil).Assemble(context.Background(), Request{
		Agent:                agentWith(agent.ToolBinding{Name: tools_usecase.RecordOptOutToolName}),
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		EntryID:              "entry-1",
		EntryType:            string(shared.EntryTypeWhatsApp),
		History:              []ai.Message{{Role: ai.RoleUser, Content: "pare"}},
	})
	if note := lastNote(out.Input); !strings.Contains(note, "indisponíveis") {
		t.Fatalf("the agent must be told it has no ids to cite:\n%s", note)
	}
}
