package agentturn

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/rag"
	"vozko/domain/tools"
	shared_usecase "vozko/usecases/shared"
)

func ragAgent(prompt string) *agent.Agent {
	return &agent.Agent{
		ID:               "a1",
		WorkspaceID:      "ws1",
		MessagingPrompt:  prompt,
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb1"},
	}
}

func TestAssembleSurvivesAnEmptyRequest(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{})

	if out.Input.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want empty", out.Input.SystemPrompt)
	}
	if out.Input.WorkspaceID != "" {
		t.Errorf("WorkspaceID = %q, want empty without an agent", out.Input.WorkspaceID)
	}
	if len(out.Input.Tools) != 0 || len(out.ToolNames) != 0 {
		t.Errorf("tools = %v, want none", out.ToolNames)
	}
	if len(out.Input.Messages) != 0 {
		t.Errorf("messages = %v, want none", out.Input.Messages)
	}
}

func TestAssembleSurvivesToolResolutionWithoutARegistry(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:                &agent.Agent{MessagingPrompt: "hi", InternalTools: []agent.ToolBinding{{Name: "x"}}},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__entry_id": "e1"},
	})

	if len(out.Input.Tools) != 0 {
		t.Errorf("tools = %v, want none without a registry", out.Input.Tools)
	}
}

func TestAssembleSurvivesRAGWithoutAService(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{Agent: ragAgent("hi"), RAGQuery: "anything"})

	if out.Input.SystemPrompt != "hi" {
		t.Errorf("SystemPrompt = %q, want the bare prompt", out.Input.SystemPrompt)
	}
}

func TestSeedsAreStampedUnderTheKeyTheAIServiceReads(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "Finish_Conversation"}}}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{
			WorkspaceID:   "ws1",
			InternalTools: []agent.ToolBinding{{Name: "Finish_Conversation"}},
		},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__entry_id": "e1", "__entry_type": "telegram"},
	})

	cfg, ok := out.Input.ToolConfigs["finish_conversation"]
	if !ok {
		t.Fatalf("no config under the lowercased name; keys = %v", keysOf(out.Input.ToolConfigs))
	}
	if cfg["__entry_id"] != "e1" || cfg["__entry_type"] != "telegram" {
		t.Errorf("seeds = %+v, want the channel context", cfg)
	}
}

func TestSeedsMergeWithTheBindingConfigRatherThanReplacingIt(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{
			InternalTools: []agent.ToolBinding{{
				Name:   "book_meeting",
				Config: map[string]interface{}{"calendar_id": "cal-9"},
			}},
		},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__entry_id": "e1"},
	})

	cfg := out.Input.ToolConfigs["book_meeting"]
	if cfg["calendar_id"] != "cal-9" {
		t.Errorf("binding config lost: %+v", cfg)
	}
	if cfg["__entry_id"] != "e1" {
		t.Errorf("seed missing: %+v", cfg)
	}
}

func TestSeedsDoNotMutateTheCallersBindingConfig(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, nil, nil)

	binding := map[string]interface{}{"calendar_id": "cal-9"}
	ag := &agent.Agent{InternalTools: []agent.ToolBinding{{Name: "book_meeting", Config: binding}}}

	a.Assemble(context.Background(), Request{
		Agent:                ag,
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__entry_id": "e1"},
	})

	if _, leaked := binding["__entry_id"]; leaked {
		t.Errorf("the agent's own binding config was mutated: %+v", binding)
	}
}

func TestSeparateAssembliesDoNotShareToolConfig(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, nil, nil)
	ag := &agent.Agent{InternalTools: []agent.ToolBinding{{
		Name: "book_meeting", Config: map[string]interface{}{"calendar_id": "cal-9"},
	}}}

	build := func(entryID string) map[string]interface{} {
		out := a.Assemble(context.Background(), Request{
			Agent:                ag,
			ResolveInternalTools: true,
			Visibility:           agent.ToolVisibilityMessaging,
			ToolSeed:             map[string]interface{}{"__entry_id": entryID},
		})
		return out.Input.ToolConfigs["book_meeting"]
	}

	first := build("entry-A")
	second := build("entry-B")

	if first["__entry_id"] != "entry-A" {
		t.Errorf("the first assembly was overwritten by the second: %+v", first)
	}
	if second["__entry_id"] != "entry-B" {
		t.Errorf("second = %+v", second)
	}
}

func TestToolsAreOmittedUnlessExplicitlyRequested(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:    &agent.Agent{InternalTools: []agent.ToolBinding{{Name: "book_meeting"}}},
		ToolSeed: map[string]interface{}{"__entry_id": "e1"},
	})

	if len(out.Input.Tools) != 0 {
		t.Errorf("tools = %v, want none when resolution was not requested", out.ToolNames)
	}
}

func TestVarsAreInterpolatedIntoTheAgentPrompt(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{MessagingPrompt: "Olá {{nome}}, tudo bem?"},
		Vars:  map[string]string{"nome": "Ana"},
	})

	if !strings.Contains(out.Input.SystemPrompt, "Ana") {
		t.Errorf("SystemPrompt = %q, want the variable resolved", out.Input.SystemPrompt)
	}
	if strings.Contains(out.Input.SystemPrompt, "{{nome}}") {
		t.Errorf("an unresolved placeholder reached the model: %q", out.Input.SystemPrompt)
	}
}

func TestIdentityReflectsResolvedToolsInThePrompt(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, nil, nil)
	identity := shared_usecase.ConversationContext{Channel: shared_usecase.ChannelMessaging, AgentName: "Bob"}

	out := a.Assemble(context.Background(), Request{
		Agent:                &agent.Agent{MessagingPrompt: "base", InternalTools: []agent.ToolBinding{{Name: "book_meeting"}}},
		Identity:             &identity,
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
	})

	if !strings.Contains(out.Input.SystemPrompt, "USO DE FERRAMENTAS") {
		t.Errorf("the tool-usage instruction is missing even though a tool resolved: %q", out.Input.SystemPrompt)
	}
	if len(identity.AvailableTools) != 0 {
		t.Errorf("the caller's identity was mutated: %+v", identity.AvailableTools)
	}
}

func TestIdentityOmitsTheToolInstructionWhenNoToolsResolved(t *testing.T) {
	a := New(stubRegistry{}, nil, nil)
	identity := shared_usecase.ConversationContext{Channel: shared_usecase.ChannelMessaging, AgentName: "Bob"}

	out := a.Assemble(context.Background(), Request{
		Agent:    &agent.Agent{MessagingPrompt: "base"},
		Identity: &identity,
	})

	if strings.Contains(out.Input.SystemPrompt, "USO DE FERRAMENTAS") {
		t.Errorf("a toolless agent was told to call tools: %q", out.Input.SystemPrompt)
	}
}

func TestWithoutIdentityThePromptIsJustTheAgentPrompt(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{Agent: &agent.Agent{MessagingPrompt: "só isso"}})

	if out.Input.SystemPrompt != "só isso" {
		t.Errorf("SystemPrompt = %q, want no preamble", out.Input.SystemPrompt)
	}
}

func TestRAGIsSkippedWithoutAQuery(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{Content: "SHOULD NOT APPEAR"}}}, nil)

	out := a.Assemble(context.Background(), Request{Agent: ragAgent("hi")})

	if strings.Contains(out.Input.SystemPrompt, "SHOULD NOT APPEAR") {
		t.Errorf("retrieval ran without a query: %q", out.Input.SystemPrompt)
	}
}

func TestExplicitKnowledgeBasesGroundAnAgentWithRAGDisabled(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{DocumentName: "faq", Content: "FROM KB", Score: 0.9}}}, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:            &agent.Agent{MessagingPrompt: "hi"},
		KnowledgeBaseIDs: []string{"kb-explicit"},
		RAGQuery:         "pergunta",
	})

	if !strings.Contains(out.Input.SystemPrompt, "FROM KB") {
		t.Errorf("explicit knowledge bases were ignored: %q", out.Input.SystemPrompt)
	}
}

func TestSuffixAlwaysFollowsGrounding(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{DocumentName: "d", Content: "GROUNDING", Score: 0.9}}}, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:        ragAgent("BASE"),
		RAGQuery:     "q",
		PromptSuffix: "\n\nTAIL",
	})

	sp := out.Input.SystemPrompt
	if i, j := strings.Index(sp, "GROUNDING"), strings.Index(sp, "TAIL"); i < 0 || j < 0 || i > j {
		t.Errorf("ordering wrong (grounding=%d tail=%d): %q", i, j, sp)
	}
}

func TestHistoryIsCopiedNotAliased(t *testing.T) {
	a := New(nil, nil, nil)
	history := make([]ai.Message, 1, 4)
	history[0] = ai.Message{Role: ai.RoleUser, Content: "primeira"}

	out := a.Assemble(context.Background(), Request{
		Agent:       &agent.Agent{MessagingPrompt: "hi"},
		History:     history,
		UserMessage: "segunda",
	})

	if len(history) != 1 {
		t.Errorf("the caller's history slice was extended: %+v", history)
	}
	if len(out.Input.Messages) != 2 {
		t.Fatalf("messages = %d, want history + the user turn", len(out.Input.Messages))
	}
	out.Input.Messages[0].Content = "mutated"
	if history[0].Content != "primeira" {
		t.Error("the assembled messages alias the caller's history")
	}
}

func TestAnEmptyUserMessageIsNotAppended(t *testing.T) {
	a := New(nil, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:       &agent.Agent{MessagingPrompt: "hi"},
		History:     []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		UserMessage: "   ",
	})

	if len(out.Input.Messages) != 1 {
		t.Errorf("messages = %+v, want only the history", out.Input.Messages)
	}
}

func TestAssemblyIsDeterministic(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "a_tool"}, {Name: "b_tool"}}}
	a := New(reg, fakeRAG{results: []rag.QueryResult{{DocumentName: "d", Content: "G", Score: 0.9}}}, nil)

	req := func() Request {
		return Request{
			Agent:                ragAgent("BASE"),
			ResolveInternalTools: true,
			Visibility:           agent.ToolVisibilityMessaging,
			ToolSeed:             map[string]interface{}{"__entry_id": "e1", "__workspace_id": "ws1"},
			RAGQuery:             "q",
		}
	}

	first := a.Assemble(context.Background(), req())
	second := a.Assemble(context.Background(), req())

	if first.Input.SystemPrompt != second.Input.SystemPrompt {
		t.Error("system prompt differs between identical assemblies")
	}
	if strings.Join(first.ToolNames, ",") != strings.Join(second.ToolNames, ",") {
		t.Errorf("tool order differs: %v vs %v", first.ToolNames, second.ToolNames)
	}
}

func keysOf(m map[string]map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestConcurrentAssembliesDoNotInterfere(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, fakeRAG{results: []rag.QueryResult{{DocumentName: "d", Content: "G", Score: 0.9}}}, nil)
	ag := &agent.Agent{
		ID: "a1", WorkspaceID: "ws1", MessagingPrompt: "base",
		RAGEnabled: true, KnowledgeBaseIDs: []string{"kb1"},
		InternalTools: []agent.ToolBinding{{
			Name: "book_meeting", Config: map[string]interface{}{"calendar_id": "cal-9"},
		}},
	}

	const workers = 32
	errs := make(chan string, workers)
	done := make(chan struct{})

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			entryID := "entry-" + string(rune('a'+i%26)) + string(rune('0'+i/26))

			out := a.Assemble(context.Background(), Request{
				Agent:                ag,
				ResolveInternalTools: true,
				Visibility:           agent.ToolVisibilityMessaging,
				ToolSeed:             map[string]interface{}{"__entry_id": entryID},
				RAGQuery:             "q",
				UserMessage:          entryID,
			})

			cfg := out.Input.ToolConfigs["book_meeting"]
			if cfg["__entry_id"] != entryID {
				errs <- "entry id crossed conversations: got " +
					toString(cfg["__entry_id"]) + " want " + entryID
			}
			if cfg["calendar_id"] != "cal-9" {
				errs <- "binding config lost under concurrency"
			}
			if n := len(out.Input.Messages); n != 1 {
				errs <- "message list corrupted under concurrency"
			}
		}(i)
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	close(errs)

	for msg := range errs {
		t.Error(msg)
	}
}

func toString(v interface{}) string {
	s, _ := v.(string)
	return s
}

type contextualStub struct {
	def  tools.Definition
	seen tools.ToolContext
}

func (c *contextualStub) Definition() tools.Definition { return c.def }
func (c *contextualStub) Execute(context.Context, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (c *contextualStub) ExecuteWithConfig(context.Context, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (c *contextualStub) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	c.seen = ctx
	d := c.def
	d.Description = "campaign=" + ctx.CampaignID
	return d
}

type contextualRegistry struct {
	defs    []tools.Definition
	handler tools.Handler
}

func (r contextualRegistry) Definitions() []tools.Definition                          { return r.defs }
func (r contextualRegistry) DefinitionsFor(v tools.ToolVisibility) []tools.Definition { return r.defs }
func (r contextualRegistry) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r contextualRegistry) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r contextualRegistry) Handler(string) (tools.Handler, bool) { return r.handler, r.handler != nil }

func TestCampaignContextReachesContextualTools(t *testing.T) {
	stub := &contextualStub{def: tools.Definition{Name: "manage_entry_stage"}}
	reg := contextualRegistry{defs: []tools.Definition{{Name: "manage_entry_stage"}}, handler: stub}
	a := New(reg, nil, nil)

	a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{
			WorkspaceID:   "ws1",
			InternalTools: []agent.ToolBinding{{Name: "manage_entry_stage"}},
		},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		CampaignID:           "camp-1",
		CampaignType:         "whatsapp",
	})

	if stub.seen.CampaignID != "camp-1" || stub.seen.CampaignType != "whatsapp" {
		t.Errorf("ToolContext = %+v, want the campaign", stub.seen)
	}
	if stub.seen.WorkspaceID != "ws1" {
		t.Errorf("workspace missing from ToolContext: %+v", stub.seen)
	}
}

func TestNoCampaignStillResolvesContextualTools(t *testing.T) {
	stub := &contextualStub{def: tools.Definition{Name: "manage_entry_stage"}}
	reg := contextualRegistry{defs: []tools.Definition{{Name: "manage_entry_stage"}}, handler: stub}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{
			WorkspaceID:   "ws1",
			InternalTools: []agent.ToolBinding{{Name: "manage_entry_stage"}},
		},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
	})

	if len(out.ToolNames) != 1 {
		t.Errorf("tools = %v, want the tool resolved without a campaign", out.ToolNames)
	}
	if stub.seen.CampaignID != "" {
		t.Errorf("a campaign appeared from nowhere: %+v", stub.seen)
	}
}

func TestPreResolvedToolsGetTheSameTreatmentAsResolvedOnes(t *testing.T) {
	a := New(nil, nil, nil)
	identity := shared_usecase.ConversationContext{Channel: shared_usecase.ChannelWhatsApp}

	out := a.Assemble(context.Background(), Request{
		Agent:              &agent.Agent{WorkspaceID: "ws1", MessagingPrompt: "base"},
		Identity:           &identity,
		PreResolved:        []tools.Definition{{Name: "Manage_Entry_Stage"}},
		PreResolvedConfigs: map[string]map[string]interface{}{"manage_entry_stage": {"pipeline": "p1"}},
		ToolSeed:           map[string]interface{}{"__entry_id": "e1"},
	})

	if len(out.Input.Tools) != 1 {
		t.Fatalf("tools = %+v, want the pre-resolved tool", out.Input.Tools)
	}
	if len(out.ToolNames) != 1 || out.ToolNames[0] != "Manage_Entry_Stage" {
		t.Errorf("ToolNames = %v", out.ToolNames)
	}
	cfg := out.Input.ToolConfigs["manage_entry_stage"]
	if cfg["pipeline"] != "p1" {
		t.Errorf("the caller's config was lost: %+v", cfg)
	}
	if cfg["__entry_id"] != "e1" {
		t.Errorf("seeds were not stamped onto pre-resolved tools: %+v", cfg)
	}
	if !strings.Contains(out.Input.SystemPrompt, "USO DE FERRAMENTAS") {
		t.Errorf("identity did not learn about the pre-resolved tools: %q", out.Input.SystemPrompt)
	}
}

func TestPreResolvedConfigsAreCopied(t *testing.T) {
	a := New(nil, nil, nil)
	callerCfg := map[string]interface{}{"pipeline": "p1"}

	a.Assemble(context.Background(), Request{
		Agent:              &agent.Agent{WorkspaceID: "ws1"},
		PreResolved:        []tools.Definition{{Name: "t"}},
		PreResolvedConfigs: map[string]map[string]interface{}{"t": callerCfg},
		ToolSeed:           map[string]interface{}{"__entry_id": "e1"},
	})

	if _, leaked := callerCfg["__entry_id"]; leaked {
		t.Errorf("the caller's resolved config was mutated: %+v", callerCfg)
	}
}

func TestResolutionTakesPrecedenceOverPreResolved(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "resolved_tool"}}}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent: &agent.Agent{
			WorkspaceID:   "ws1",
			InternalTools: []agent.ToolBinding{{Name: "resolved_tool"}},
		},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		PreResolved:          []tools.Definition{{Name: "ignored_tool"}},
	})

	if len(out.ToolNames) != 1 || out.ToolNames[0] != "resolved_tool" {
		t.Errorf("ToolNames = %v, want only the resolved tool", out.ToolNames)
	}
}

func TestAgentWithNoBoundToolsResolvesToAnExplicitlyEmptySet(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}, {Name: "http_request"}}}
	a := New(reg, nil, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:                &agent.Agent{InternalTools: []agent.ToolBinding{}},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
	})

	if out.Input.Tools == nil {
		t.Fatal("tools = nil for an agent that binds none; the AI service reads nil as 'use the full registry'")
	}
	if len(out.Input.Tools) != 0 {
		t.Fatalf("tools = %v, want an empty set", out.ToolNames)
	}
}
