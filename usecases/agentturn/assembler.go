package agentturn

import (
	"context"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/rag"
	"vozko/domain/tools"
	lead_memory_usecase "vozko/usecases/lead_memory"
	rag_usecase "vozko/usecases/rag"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"
)

type Assembler struct {
	tools    tools.Service
	rag      rag.RAGService
	memories leadmemory.ListUseCase
}

func New(toolRegistry tools.Service, ragService rag.RAGService, memories leadmemory.ListUseCase) *Assembler {
	return &Assembler{tools: toolRegistry, rag: ragService, memories: memories}
}

type Request struct {
	Agent *agent.Agent
	Vars  map[string]string

	Identity *shared_usecase.ConversationContext

	ResolveInternalTools bool
	Visibility           agent.ToolVisibility
	ToolSeed             map[string]interface{}
	CampaignID           string
	CampaignType         string

	PreResolved        []tools.Definition
	PreResolvedConfigs map[string]map[string]interface{}

	RAGQuery         string
	KnowledgeBaseIDs []string

	LeadID string

	PromptSuffix string

	History     []ai.Message
	UserMessage string

	Model       string
	Temperature float32
	Segmented   bool
}

type Assembled struct {
	Input     ai.GenerateInput
	ToolNames []string
}

func (a *Assembler) Assemble(ctx context.Context, req Request) Assembled {
	basePrompt := ""
	if req.Agent != nil {
		basePrompt = strings.TrimSpace(req.Agent.MessagingPrompt)
		interpolated := agent.InterpolateAgent(req.Agent, req.Vars)
		if interpolated.MessagingPrompt != "" {
			basePrompt = interpolated.MessagingPrompt
		}
	}

	var toolDefs []tools.Definition
	toolConfigs := make(map[string]map[string]interface{})
	var toolNames []string

	switch {
	case req.ResolveInternalTools && req.Agent != nil:
		resolved := tools_usecase.ResolveToolsWithOptions(a.tools, req.Agent.InternalTools, req.Visibility, tools_usecase.ToolResolverOptions{
			Agent:        req.Agent,
			CampaignID:   req.CampaignID,
			CampaignType: req.CampaignType,
		})
		toolDefs = resolved.Definitions
		if toolDefs == nil {
			toolDefs = []tools.Definition{}
		}
		copyConfigsInto(toolConfigs, resolved.Configs)

	case len(req.PreResolved) > 0:
		toolDefs = req.PreResolved
		copyConfigsInto(toolConfigs, req.PreResolvedConfigs)
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

	systemPrompt := basePrompt
	if req.Identity != nil {
		id := *req.Identity
		id.AvailableTools = toolNames
		systemPrompt = id.BuildContextPrompt() + basePrompt
	}

	if ragCtx := rag_usecase.BuildContext(ctx, a.rag, rag_usecase.ContextInput{
		Agent:            req.Agent,
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
		Query:            req.RAGQuery,
	}); ragCtx != "" {
		systemPrompt += ragCtx
	}

	if req.LeadID != "" && req.Agent != nil {
		if memCtx := lead_memory_usecase.BuildContext(ctx, a.memories, lead_memory_usecase.ContextInput{
			WorkspaceID:   req.Agent.WorkspaceID,
			LeadID:        req.LeadID,
			HasMemoryTool: containsToolName(toolNames, tools_usecase.ManageLeadMemoryToolName),
		}); memCtx != "" {
			systemPrompt += memCtx
		}
	}

	if strings.TrimSpace(req.PromptSuffix) != "" {
		systemPrompt += req.PromptSuffix
	}

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

func containsToolName(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}

func copyConfigsInto(dst map[string]map[string]interface{}, src map[string]map[string]interface{}) {
	for name, cfg := range src {
		copied := make(map[string]interface{}, len(cfg))
		for k, v := range cfg {
			copied[k] = v
		}
		dst[strings.ToLower(name)] = copied
	}
}
