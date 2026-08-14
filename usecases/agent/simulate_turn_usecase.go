package agent_usecase

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/usecases/agentturn"
	lead_memory_usecase "vozko/usecases/lead_memory"
	rag_usecase "vozko/usecases/rag"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"
)

// simulationEntryID is the conversation identity a simulated turn presents to
// the prompt. It is deliberately not seeded as __entry_id: entry-scoped tools
// are sandboxed anyway, and a fake entry id reaching a real repository would
// be worse than an absent one.
const simulationEntryID = "simulacao"

type simulateTurnUseCase struct {
	agents    agent.Repository
	assembler *agentturn.Assembler
	// ai is the SIMULATION service: same provider, same tool loop, but wired
	// with SimulatedToolService so every execution is intercepted. Handing this
	// use case the production service would silently turn the simulator into a
	// live channel, hence the dedicated constructor argument.
	ai ai.Service
}

// NewSimulateTurnUseCase wires the simulator. The assembler is the SAME shared
// recipe production uses (that equivalence is the product: what you debug is
// what ships); the AI service must be the sandboxed one.
func NewSimulateTurnUseCase(agents agent.Repository, assembler *agentturn.Assembler, sandboxedAI ai.Service) (agent.SimulateTurnUseCase, error) {
	missing := []string{}
	if agents == nil {
		missing = append(missing, "agent repository")
	}
	if assembler == nil {
		missing = append(missing, "turn assembler")
	}
	if sandboxedAI == nil {
		missing = append(missing, "sandboxed AI service")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("agent simulate use case: missing %s", strings.Join(missing, ", "))
	}
	return &simulateTurnUseCase{agents: agents, assembler: assembler, ai: sandboxedAI}, nil
}

func (uc *simulateTurnUseCase) Execute(ctx context.Context, in agent.SimulateTurnInput) (*agent.SimulateTurnOutput, error) {
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return nil, agent.ErrSimulationMessageRequired
	}
	if len(in.History) > agent.MaxSimulationHistory {
		return nil, agent.ErrSimulationHistoryTooLong
	}
	if len(in.SessionMemories) > agent.MaxSimulationHistory {
		return nil, agent.ErrSimulationMemoriesTooLong
	}

	agentRecord, err := uc.agents.FindByID(in.AgentID)
	if err != nil || agentRecord == nil {
		return nil, agent.ErrAgentNotFound
	}
	// Authorization boundary: a foreign agent behaves like a missing one.
	if agentRecord.WorkspaceID != in.WorkspaceID {
		return nil, agent.ErrAgentNotFound
	}

	history := make([]ai.Message, 0, len(in.History))
	for _, m := range in.History {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		role := ai.RoleUser
		if m.Role == agent.SimulationRoleAssistant {
			role = ai.RoleAssistant
		}
		history = append(history, ai.Message{Role: role, Content: content})
	}

	identity := shared_usecase.ConversationContext{
		// Messaging, like every adapter-backed channel: the simulator stands in
		// for "some messaging surface", not for WhatsApp specifically.
		Channel:        shared_usecase.ChannelMessaging,
		AgentName:      agentRecord.Name,
		ConversationID: simulationEntryID,
	}

	leadID := strings.TrimSpace(in.LeadID)
	seed := map[string]interface{}{
		"__workspace_id": agentRecord.WorkspaceID,
		"__agent_id":     agentRecord.ID,
		// Honest flag for anything downstream that ever wants to know; today
		// nothing reads it because interception happens at the service.
		"__simulation": true,
	}
	if leadID != "" {
		seed["__lead_id"] = leadID
	}

	assembled := uc.assembler.Assemble(ctx, agentturn.Request{
		Agent:    agentRecord,
		Identity: &identity,

		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             seed,

		RAGQuery: message,
		LeadID:   leadID,

		// This session's remembered facts ride the caller-tail slot, rendered
		// by the same renderer real memories use. With a lead selected, its
		// real memories (assembler) and the session's (here) both appear.
		PromptSuffix: uc.sessionMemoryBlock(agentRecord, in.SessionMemories),

		History:     history,
		UserMessage: message,

		Model:     agentRecord.MessagingModel,
		Segmented: true,
	})

	input := assembled.Input
	input.ToolExecutionMode = ai.ToolExecutionModeAuto
	input.WorkspaceID = agentRecord.WorkspaceID

	out, err := uc.ai.Generate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("agent simulation: %w", err)
	}

	replies := out.Messages
	if len(replies) == 0 && strings.TrimSpace(out.Message.Content) != "" {
		replies = []string{out.Message.Content}
	}

	toolCalls := make([]agent.SimulatedToolCall, 0, len(out.ToolCalls))
	for _, call := range out.ToolCalls {
		sim := agent.SimulatedToolCall{Name: call.Name, Arguments: call.Arguments}
		if call.Result != nil {
			sim.Result = stringifyResult(call.Result.Result)
			sim.IsError = call.Result.IsError
		}
		toolCalls = append(toolCalls, sim)
	}

	return &agent.SimulateTurnOutput{
		Replies:   replies,
		ToolCalls: toolCalls,
		Debug: agent.SimulationDebug{
			Model:            agentRecord.MessagingModel,
			SystemPrompt:     input.SystemPrompt,
			ToolNames:        assembled.ToolNames,
			MemoryInjected:   strings.Contains(input.SystemPrompt, lead_memory_usecase.ContextHeader),
			RAGInjected:      strings.Contains(input.SystemPrompt, rag_usecase.ContextHeader),
			PromptTokens:     out.Usage.PromptTokens,
			CompletionTokens: out.Usage.CompletionTokens,
			FinishReason:     out.FinishReason,
		},
	}, nil
}

// sessionMemoryIDPattern keeps client-chosen memory ids honest: short, lowercase
// alphanumeric (plus dash), so the prompt block renders clean stable prefixes.
var sessionMemoryIDPattern = regexp.MustCompile(`^[a-z0-9-]{8,36}$`)

// sessionMemoryBlock renders this session's remembered facts through the SAME
// renderer real memories use, so the model sees an indistinguishable block and
// the update/forget half of the tool keeps working against stable short ids.
func (uc *simulateTurnUseCase) sessionMemoryBlock(agentRecord *agent.Agent, memories []agent.SessionMemory) string {
	items := make([]leadmemory.MemoryView, 0, len(memories))
	now := time.Now()
	for i, sm := range memories {
		content := strings.TrimSpace(sm.Content)
		if content == "" {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(sm.ID))
		if !sessionMemoryIDPattern.MatchString(id) {
			id = fmt.Sprintf("sim%05d-0000-4000-8000-000000000000", i)
		}
		category := leadmemory.Category(strings.ToLower(strings.TrimSpace(sm.Category)))
		if !category.Valid() {
			category = leadmemory.CategoryOther
		}
		items = append(items, leadmemory.MemoryView{LeadMemory: &leadmemory.LeadMemory{
			ID:        id,
			Category:  category,
			Content:   content,
			CreatedAt: now,
		}})
	}
	if len(items) == 0 {
		return ""
	}

	hasMemoryTool := false
	for _, binding := range agentRecord.InternalTools {
		if strings.EqualFold(binding.Name, tools_usecase.ManageLeadMemoryToolName) {
			hasMemoryTool = true
			break
		}
	}
	return lead_memory_usecase.FormatMemoryContext(items, int64(len(items)), hasMemoryTool)
}

func stringifyResult(result interface{}) string {
	switch v := result.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

var _ agent.SimulateTurnUseCase = (*simulateTurnUseCase)(nil)
