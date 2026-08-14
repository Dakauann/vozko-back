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

// The assembler is about to become the single recipe every channel's agent turn
// is built from. Until now it has never run outside its own unit test, so these
// cover the paths a real channel will actually take, including the ones where a
// caller passes nothing, which is exactly what a partially-wired container does.

func ragAgent(prompt string) *agent.Agent {
	return &agent.Agent{
		ID:               "a1",
		WorkspaceID:      "ws1",
		MessagingPrompt:  prompt,
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb1"},
	}
}

// ---------------------------------------------------------------- robustness

// A container that has not finished wiring, or a channel with no agent
// configured, must not take the process down.
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

// ResolveInternalTools with no registry is what a channel adopting this before
// the container passes one would do.
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

// ---------------------------------------------------------------- tool config

// The AI service reads ToolConfigs[strings.ToLower(name)]. A seed stamped under
// any other key is silently invisible at execution time, the tool runs with no
// entry id and fails or, worse, acts on the wrong conversation.
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

// A tool with its own binding config must keep it AND receive the seeds: the
// binding is what the operator configured, the seed is which conversation it is
// acting on. Losing either one is a different kind of wrong.
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

// Mutating the caller's binding config through the assembled output would leak
// one conversation's entry id into the agent record shared by all of them.
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

// Two turns on two different conversations must not see each other's seeds.
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
		Agent: &agent.Agent{InternalTools: []agent.ToolBinding{{Name: "book_meeting"}}},
		// ResolveInternalTools deliberately false.
		ToolSeed: map[string]interface{}{"__entry_id": "e1"},
	})

	if len(out.Input.Tools) != 0 {
		t.Errorf("tools = %v, want none when resolution was not requested", out.ToolNames)
	}
}

// ---------------------------------------------------------------- prompt

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

// The identity preamble is built from the RESOLVED tool names, so it must come
// after resolution. Built first, it would see no tools and omit the "you must
// call the function, never just say you will" instruction, while the model is
// handed a full tool list. That combination produces an agent that narrates
// actions it never performs, which is the single most confusing failure an
// operator can be asked to debug.
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
	// The caller's own struct must not be mutated, a ConversationContext is
	// typically built per agent and reused across turns.
	if len(identity.AvailableTools) != 0 {
		t.Errorf("the caller's identity was mutated: %+v", identity.AvailableTools)
	}
}

// The mirror image: no tools resolved means the instruction must be absent, or
// every toolless agent is told to call functions it does not have.
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

// ---------------------------------------------------------------- RAG

func TestRAGIsSkippedWithoutAQuery(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{Content: "SHOULD NOT APPEAR"}}}, nil)

	out := a.Assemble(context.Background(), Request{Agent: ragAgent("hi")}) // no RAGQuery

	if strings.Contains(out.Input.SystemPrompt, "SHOULD NOT APPEAR") {
		t.Errorf("retrieval ran without a query: %q", out.Input.SystemPrompt)
	}
}

// An agent with RAG off can still be grounded against explicitly named bases,
// this is the path a channel uses when the knowledge base comes from the
// channel account rather than the agent.
func TestExplicitKnowledgeBasesGroundAnAgentWithRAGDisabled(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{DocumentName: "faq", Content: "FROM KB", Score: 0.9}}}, nil)

	out := a.Assemble(context.Background(), Request{
		Agent:            &agent.Agent{MessagingPrompt: "hi"}, // RAG disabled on the agent
		KnowledgeBaseIDs: []string{"kb-explicit"},
		RAGQuery:         "pergunta",
	})

	if !strings.Contains(out.Input.SystemPrompt, "FROM KB") {
		t.Errorf("explicit knowledge bases were ignored: %q", out.Input.SystemPrompt)
	}
}

// The suffix carries "actions already taken" and must land after grounding, or
// the model reads stale instructions as the most recent context.
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

// ---------------------------------------------------------------- messages

func TestHistoryIsCopiedNotAliased(t *testing.T) {
	a := New(nil, nil, nil)
	history := make([]ai.Message, 1, 4) // spare capacity: an append would overwrite in place
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
	// Mutating the output must not reach back into the caller's slice.
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

// ---------------------------------------------------------------- determinism

// Same request, same output. Map iteration over tool configs must not leak
// ordering into the prompt or the tool list.
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

// One Assembler is shared by every conversation on every channel. Nothing in it
// may be per-turn state: two contacts assembling at the same instant must not
// see each other's entry id, and the RAG/tool lookups must stay read-only.
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

// contextualStub is a tool whose DEFINITION depends on the campaign, the shape
// manage_entry_stage has, where the campaign's pipeline fills the stage enum.
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

// The campaign reaches the resolver's ToolContext, or a contextual tool is
// offered with an empty enum while the prompt tells the model to use only
// enumerated values, an agent that silently stops classifying leads.
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

// A channel with no campaign (Telegram, Instagram) must still resolve tools.
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

// WhatsApp resolves its tools earlier, with campaign context the assembler does
// not have. Supplying them pre-resolved must get the SAME treatment as
// self-resolved ones, seeds stamped, names collected, identity informed, or
// the surface that pre-resolves quietly loses all three.
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
	// The identity must know it has tools, or the model is told to answer in
	// prose while holding a tool list.
	if !strings.Contains(out.Input.SystemPrompt, "USO DE FERRAMENTAS") {
		t.Errorf("identity did not learn about the pre-resolved tools: %q", out.Input.SystemPrompt)
	}
}

// Pre-resolved configs must be copied, not referenced: the caller reuses its
// resolved set across turns.
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

// Resolution wins when both are supplied, a caller means one or the other, and
// silently merging two tool sets would offer duplicates to the model.
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
