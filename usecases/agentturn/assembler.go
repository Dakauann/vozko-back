package agentturn

import (
	"context"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/tools"
	lead_memory_usecase "vozko/usecases/lead_memory"
	rag_usecase "vozko/usecases/rag"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"
)

type Assembler struct {
	tools     tools.Service
	rag       rag.RAGService
	memories  leadmemory.ListUseCase
	citable   tools_usecase.RecentMessages
	agentRuns AgentRunContext
}

type AgentRunContext interface {
	ContextLine(workspaceID, entryID, entryType string) string
}

const agentRunHeading = "FLUXO QUE VOCÊ EXECUTOU NESTA CONVERSA:"

func (a *Assembler) TrackAgentRuns(runs AgentRunContext) {
	a.agentRuns = runs
}

func New(toolRegistry tools.Service, ragService rag.RAGService, memories leadmemory.ListUseCase) *Assembler {
	return &Assembler{tools: toolRegistry, rag: ragService, memories: memories}
}

func (a *Assembler) CiteMessagesFrom(messages tools_usecase.RecentMessages) {
	a.citable = messages
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
	EntryID              string
	EntryType            string

	PreResolved        []tools.Definition
	PreResolvedConfigs map[string]map[string]interface{}

	RAGQuery         string
	KnowledgeBaseIDs []string

	LeadID string

	PromptSuffix string

	Session string

	History     []ai.Message
	UserMessage string
	TriggerNote string

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
			EntryID:      req.EntryID,
			EntryType:    req.EntryType,
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

	messages, volatileTail := a.conversation(ctx, req, a.citations(req, toolNames), a.agentRunSection(req, toolDefs))

	workspaceID := ""
	if req.Agent != nil {
		workspaceID = req.Agent.WorkspaceID
	}

	return Assembled{
		Input: ai.GenerateInput{
			WorkspaceID:       workspaceID,
			Model:             req.Model,
			SystemPrompt:      a.systemPrompt(ctx, req, basePrompt, toolNames),
			Messages:          messages,
			Temperature:       req.Temperature,
			Tools:             toolDefs,
			ToolConfigs:       toolConfigs,
			SegmentedResponse: req.Segmented,
			BillingReference:  req.Session,
			SessionID:         req.Session,
			VolatileTail:      volatileTail,
			AgentTurn:         req.agentTurn(),
		},
		ToolNames: toolNames,
	}
}

const contextNoteTail = 1

func (a *Assembler) systemPrompt(ctx context.Context, req Request, basePrompt string, toolNames []string) string {
	var identity *shared_usecase.ConversationContext
	if req.Identity != nil {
		withTools := *req.Identity
		withTools.AvailableTools = toolNames
		identity = &withTools
	}

	var sb strings.Builder
	if identity != nil {
		sb.WriteString(identity.RulesPrompt())
	}
	sb.WriteString(basePrompt)
	if identity != nil {
		sb.WriteString(identity.LeadPrompt())
	}
	if req.LeadID != "" && req.Agent != nil {
		sb.WriteString(lead_memory_usecase.BuildContext(ctx, a.memories, lead_memory_usecase.ContextInput{
			WorkspaceID:   req.Agent.WorkspaceID,
			LeadID:        req.LeadID,
			HasMemoryTool: containsToolName(toolNames, tools_usecase.ManageLeadMemoryToolName),
		}))
	}
	if strings.TrimSpace(req.PromptSuffix) != "" {
		sb.WriteString(req.PromptSuffix)
	}
	return sb.String()
}

func (a *Assembler) citations(req Request, toolNames []string) string {
	if !tools_usecase.CitesMessages(toolNames) {
		return ""
	}
	entryID, entryType := req.conversationEntry()
	at := tools_usecase.CitableConversation{EntryID: entryID, EntryType: shared.EntryType(entryType)}
	if req.Agent != nil {
		at.AgentID = req.Agent.ID
	}
	return tools_usecase.CitableMessagesNote(a.citable, at)
}

func (req Request) conversationEntry() (string, string) {
	if req.EntryID != "" {
		return req.EntryID, req.EntryType
	}
	entryID, _ := req.ToolSeed["__entry_id"].(string)
	entryType, _ := req.ToolSeed["__entry_type"].(string)
	return entryID, entryType
}

func (req Request) agentTurn() ai.AgentTurn {
	entryID, entryType := req.conversationEntry()
	turn := ai.AgentTurn{EntryID: entryID, EntryType: entryType, CampaignID: req.CampaignID}
	if req.Agent != nil {
		turn.AgentID = req.Agent.ID
	}
	return turn
}

func (a *Assembler) agentRunSection(req Request, defs []tools.Definition) string {
	if a.agentRuns == nil || req.Agent == nil || !offersWorkflowRuns(defs) {
		return ""
	}
	entryID, entryType := req.conversationEntry()
	line := a.agentRuns.ContextLine(req.Agent.WorkspaceID, entryID, entryType)
	if line == "" {
		return ""
	}
	return agentRunHeading + "\n" + line
}

func offersWorkflowRuns(defs []tools.Definition) bool {
	for _, def := range defs {
		if strings.EqualFold(def.Name, tools_usecase.RunWorkflowToolName) || strings.EqualFold(def.HandlerName, tools_usecase.RunWorkflowToolName) {
			return true
		}
	}
	return false
}

func (a *Assembler) conversation(ctx context.Context, req Request, citations, agentRun string) ([]ai.Message, int) {
	messages := append([]ai.Message(nil), req.History...)
	if strings.TrimSpace(req.UserMessage) != "" {
		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: req.UserMessage})
	}
	if len(messages) == 0 {
		return messages, 0
	}

	grounding := rag_usecase.BuildContext(ctx, a.rag, rag_usecase.ContextInput{
		Agent:            req.Agent,
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
		Query:            req.RAGQuery,
	})
	return append(messages, ai.ContextNote(grounding, citations, agentRun, req.TriggerNote)), contextNoteTail
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
