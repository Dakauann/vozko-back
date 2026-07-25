// Package agentturn centralizes the assembly of a single agent LLM turn:
// grounded system prompt, resolved tools with channel config seeds, model, and
// message list, returned as a ready ai.GenerateInput.
//
// It is build-only: it never calls the model. Each channel (WhatsApp webhook,
// VoIP bridge, workflow node, agent simulator) is a thin driving adapter that
// fills a Request and owns execution (Generate vs GenerateStream), delivery, and
// guards. One recipe means channels cannot drift, e.g. one path silently
// forgetting to inject the knowledge base.
package agentturn

import (
	"context"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/rag"
	"vozko/domain/tools"
	rag_usecase "vozko/usecases/rag"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"
)

// Assembler composes agent turns from the shared building blocks. It depends only
// on the tool registry and the RAG service, so it stays transport-agnostic.
type Assembler struct {
	tools tools.Service
	rag   rag.RAGService
}

func New(toolRegistry tools.Service, ragService rag.RAGService) *Assembler {
	return &Assembler{tools: toolRegistry, rag: ragService}
}

// Request is the channel-agnostic description of one turn. Adapters set the parts
// that vary; the Assembler owns the invariant assembly.
type Request struct {
	// Agent drives the prompt source (messaging/system prompt), tool bindings and
	// agent-scoped RAG. Interpolated with Vars.
	Agent *agent.Agent
	Vars  map[string]string

	// Identity, when non-nil, prepends the channel context prompt (voice/whatsapp/
	// messaging preamble + identity/tool rules) ahead of the agent prompt. Its
	// AvailableTools field is filled by the Assembler from the resolved tools.
	Identity *shared_usecase.ConversationContext

	// Tools: when ResolveInternalTools is set, the agent's internal (native + MCP)
	// tools are resolved for Visibility and ToolSeed is stamped onto every resolved
	// tool's config (e.g. __workspace_id, __recipient_phone, __entry_id).
	ResolveInternalTools bool
	Visibility           agent.ToolVisibility
	ToolSeed             map[string]interface{}

	// RAG: Query drives retrieval (agent KBs win; else KnowledgeBaseIDs). Empty
	// query disables it.
	RAGQuery         string
	KnowledgeBaseIDs []string

	// PromptSuffix is appended after the RAG block (e.g. an "actions already taken"
	// note). Kept separate so it always lands after grounding.
	PromptSuffix string

	// Conversation.
	History     []ai.Message
	UserMessage string // appended as the final user turn when non-empty

	// Generation knobs the assembler fills into the returned input. Execution-only
	// knobs (ToolExecutionMode, ToolChoice, MaxTokens, MaxToolIterations) are left
	// for the caller to set on Assembled.Input.
	Model       string
	Temperature float32
	Segmented   bool
}

// Assembled is the ready-to-run request plus a little metadata for logging.
type Assembled struct {
	Input     ai.GenerateInput
	ToolNames []string
}

// Assemble runs the invariant recipe: interpolate prompt, resolve tools + seed
// configs, prepend channel context, inject RAG, append suffix, build messages.
func (a *Assembler) Assemble(ctx context.Context, req Request) Assembled {
	// 1. Base agent prompt (interpolated).
	basePrompt := ""
	if req.Agent != nil {
		basePrompt = strings.TrimSpace(req.Agent.MessagingPrompt)
		interpolated := agent.InterpolateAgent(req.Agent, req.Vars)
		if interpolated.MessagingPrompt != "" {
			basePrompt = interpolated.MessagingPrompt
		}
	}

	// 2. Resolve tools and stamp channel seeds onto each tool's config.
	var toolDefs []tools.Definition
	toolConfigs := make(map[string]map[string]interface{})
	var toolNames []string
	if req.ResolveInternalTools && req.Agent != nil {
		resolved := tools_usecase.ResolveToolsWithOptions(a.tools, req.Agent.InternalTools, req.Visibility, tools_usecase.ToolResolverOptions{Agent: req.Agent})
		toolDefs = resolved.Definitions
		for name, cfg := range resolved.Configs {
			copied := make(map[string]interface{}, len(cfg))
			for k, v := range cfg {
				copied[k] = v
			}
			toolConfigs[name] = copied
		}
		for _, def := range toolDefs {
			toolNames = append(toolNames, def.Name)
			key := strings.ToLower(def.Name)
			if toolConfigs[key] == nil {
				toolConfigs[key] = make(map[string]interface{})
			}
			for sk, sv := range req.ToolSeed {
				toolConfigs[key][sk] = sv
			}
		}
	}

	// 3. Channel context prompt (aware of the resolved tool names) then agent prompt.
	systemPrompt := basePrompt
	if req.Identity != nil {
		id := *req.Identity
		id.AvailableTools = toolNames
		systemPrompt = id.BuildContextPrompt() + basePrompt
	}

	// 4. Ground in the knowledge base (single source of RAG for all channels).
	if ragCtx := rag_usecase.BuildContext(ctx, a.rag, rag_usecase.ContextInput{
		Agent:            req.Agent,
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
		Query:            req.RAGQuery,
	}); ragCtx != "" {
		systemPrompt += ragCtx
	}

	// 5. Caller-supplied tail (executed actions, etc.), always after grounding.
	if strings.TrimSpace(req.PromptSuffix) != "" {
		systemPrompt += req.PromptSuffix
	}

	// 6. Messages.
	messages := append([]ai.Message(nil), req.History...)
	if strings.TrimSpace(req.UserMessage) != "" {
		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: req.UserMessage})
	}

	workspaceID := ""
	if req.Agent != nil {
		workspaceID = req.Agent.WorkspaceID
	}

	return Assembled{
		Input: ai.GenerateInput{
			WorkspaceID:       workspaceID,
			Model:             req.Model,
			SystemPrompt:      systemPrompt,
			Messages:          messages,
			Temperature:       req.Temperature,
			Tools:             toolDefs,
			ToolConfigs:       toolConfigs,
			SegmentedResponse: req.Segmented,
		},
		ToolNames: toolNames,
	}
}
