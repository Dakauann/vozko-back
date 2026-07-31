package node_executors

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/rag"
	"vozko/domain/shared"
	tools "vozko/domain/tools"
	"vozko/domain/workflow"
	rag_usecase "vozko/usecases/rag"
)

// WorkflowAIAttendance records AI-as-attendant metrics from workflow AI nodes (queue only).
type WorkflowAIAttendance interface {
	RecordAIReply(in aa.StartInput, messageID string)
}

const aiAgentDefaultContextWindow = 50
const aiAgentTimeout = 90 * time.Second

const (
	segmentedTypingDelay   = 1500 * time.Millisecond
	segmentedTypingMinShow = 1000 * time.Millisecond
)

type aiAgentExecutor struct {
	aiService            ai.Service
	agentRepo            agent.Repository
	messageRepo          conversation.MessageRepository
	cachedBalanceChecker balance.CachedBalanceChecker
	sender               *channelSender
	waSender             *whatsappSender
	ragService           rag.RAGService
	aiAttendance         WorkflowAIAttendance
}

func NewAIAgentExecutor(aiSvc ai.Service, agentRepo agent.Repository, msgRepo conversation.MessageRepository, cachedBalanceChecker balance.CachedBalanceChecker, waSender *whatsappSender, ragSvc rag.RAGService) workflow.NodeExecutor {
	return &aiAgentExecutor{
		aiService:            aiSvc,
		agentRepo:            agentRepo,
		messageRepo:          msgRepo,
		cachedBalanceChecker: cachedBalanceChecker,
		sender:               newChannelSenderFromWhatsApp(waSender),
		waSender:             waSender,
		ragService:           ragSvc,
	}
}

// SetAIAttendance wires session metrics for WhatsApp workflow AI nodes.
func (e *aiAgentExecutor) SetAIAttendance(svc WorkflowAIAttendance) {
	if e != nil {
		e.aiAttendance = svc
	}
}

// SetAIAttendanceOnExecutor sets attendance on a registered executor if supported.
func SetAIAttendanceOnExecutor(exec workflow.NodeExecutor, svc WorkflowAIAttendance) {
	if setter, ok := exec.(interface{ SetAIAttendance(WorkflowAIAttendance) }); ok {
		setter.SetAIAttendance(svc)
	}
}

func (e *aiAgentExecutor) recordWorkflowAIAttendance(ctx *workflow.NodeContext, agentID, model, messageID string) {
	if e == nil || e.aiAttendance == nil || ctx == nil || ctx.Run == nil {
		return
	}
	wsID := strings.TrimSpace(ctx.Run.WorkspaceID)
	entryID := strings.TrimSpace(ctx.Run.EntryID)
	if wsID == "" || entryID == "" {
		return
	}
	entryType := strings.TrimSpace(ctx.Run.EntryType)
	if entryType == "" {
		entryType = string(shared.EntryTypeWhatsApp)
	}
	// Prompt-mode nodes have no saved agent: use workflow id as stable actor.
	agentID = strings.TrimSpace(agentID)
	if agentID == "" && ctx.Run.WorkflowID != "" {
		agentID = ctx.Run.WorkflowID
	}
	if agentID == "" {
		return
	}
	const channel = "whatsapp"
	if messageID == "" {
		messageID = "wf-ai-" + ctx.Node.ID
	}
	e.aiAttendance.RecordAIReply(aa.StartInput{
		WorkspaceID: wsID,
		EntryID:     entryID,
		EntryType:   entryType,
		AgentID:     agentID,
		Channel:     channel,
		CampaignID:  "",
		Model:       model,
	}, messageID)
}

func (e *aiAgentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionAIAgent,
		Category:    workflow.NodeCategoryAI,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Agente IA",
		Description: "Gera uma resposta usando o agente de IA e salva em uma variável.",
		Icon:        "Robot",
		Guidance: workflow.NodeGuidance{
			When: "Para conversar com o contato usando IA, classificar intenção ou orquestrar ferramentas. source=prompt (defina model+instructions) para um agente ad-hoc; source=agent (defina agent_id) para reutilizar um agente salvo.",
			Behavior: "ENVIO: em response_mode=segmented o PRÓPRIO NÓ envia as mensagens no WhatsApp (divididas, com indicador de 'digitando') — não conecte um send_text depois (envio duplicado, é bloqueado). " +
				"Em response_mode=default o nó também envia a resposta no WhatsApp; use um send_text apenas para enviar por outro canal, transformar o texto, ou quando o agente roteia para ferramentas (aí o nó NÃO envia). " +
				"tool_mode (campo oculto, essencial quando há custom_tools): 'route' = a IA escolhe uma ferramenta e o fluxo SAI pela saída cujo nome é o da ferramenta; 'execute' = as ferramentas rodam internamente e o fluxo segue pela saída padrão. Defina custom_tools e tool_mode ANTES de conectar. " +
				"Ao chamar uma ferramenta, cada argumento fica acessível adiante como {{node.<id>.<arg>}} (ex.: {{node.n2.cep}}).",
			Examples: []string{
				"config: {\"source\":\"prompt\",\"model\":\"<find_resource ai_models>\",\"instructions\":\"Você é um atendente cordial de cartões...\",\"response_mode\":\"segmented\"}  // o nó envia sozinho; ligue só a 'default' (ex.: wait_for_reply)",
				"Ferramenta + HTTP: ai_agent(tool_mode=route, custom_tools=[{name:buscar_cep, parameters:[{name:cep}]}]) — conecte a saída \"buscar_cep\" → http_request com url usando {{node.<idDoAgente>.cep}}; conecte o \"sucesso\" do http de volta ao agente.",
			},
		},
		DefaultConfig: map[string]interface{}{
			"input_mode":         "conversation",
			"response_mode":      "default",
			"text":               "{{message}}",
			"include_history":    "yes",
			"source":             "agent",
			"agent_id":           "",
			"model":              "",
			"instructions":       "",
			"additional_context": "",
			"context_window":     float64(aiAgentDefaultContextWindow),
			"response_variable":  "ai_response",
			"knowledge_base_ids": []interface{}{},
			"mcp_collection_ids": []interface{}{},
			"custom_tools":       []interface{}{},
		},
		// Base handles ship in the catalog so they render instantly. The full set
		// (these + one route per custom tool) is config-dependent — DynamicHandles
		// tells the frontend to resolve it via the backend (AIAgentToolOutputs).
		Outputs: []workflow.HandleDefinition{
			{ID: "default", Label: "Resposta (texto)"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		DynamicHandles: true,
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "response_text", Description: "Resposta gerada pela IA"},
			{Key: "messages", Description: "Lista de mensagens segmentadas (quando modo segmentado)"},
			{Key: "delivered", Description: "true se as mensagens foram enviadas diretamente pelo nó"},
			{Key: "agent_id", Description: "ID do agente utilizado"},
			{Key: "model", Description: "Modelo de IA utilizado"},
			{Key: "tool_name", Description: "Nome da ferramenta chamada pela IA (se houver)"},
			{Key: "tool_args", Description: "Argumentos da ferramenta chamada (objeto JSON)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "input_mode", Label: "Modo de entrada", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "conversation", Label: "Conversa (histórico completo)"},
				{Value: "custom", Label: "Texto personalizado"},
			}},
			{Key: "response_mode", Label: "Modo de resposta", Type: "select", Description: "Segmentada: divide a resposta em múltiplas mensagens curtas. No WhatsApp, envia cada mensagem com indicador de digitação automaticamente.", Options: []workflow.ConfigFieldOption{
				{Value: "default", Label: "Padrão (uma mensagem)"},
				{Value: "segmented", Label: "Segmentada (múltiplas mensagens + digitação)"},
			}},
			{Key: "text", Label: "Texto de entrada", Type: "textarea", Placeholder: "Ex: {{last.body}}, {{var.api_result}}, {{message}}"},
			{Key: "include_history", Label: "Incluir histórico", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "no", Label: "Não"},
				{Value: "yes", Label: "Sim"},
			}},
			{Key: "source", Label: "Fonte de IA", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "agent", Label: "Agente salvo"},
				{Value: "prompt", Label: "Prompt inline"},
			}, Description: "Modo do nó (padrão 'agent' quando vazio). 'agent': usa um agente salvo — preencha agent_id; model/instructions são ignorados. 'prompt': agente ad-hoc — preencha model e instructions; agent_id é ignorado."},
			{Key: "agent_id", Label: "Agente", Type: "select", Placeholder: "Selecione um agente", OptionsSource: "agents"},
			{Key: "model", Label: "Modelo", Type: "select", Placeholder: "Selecione um modelo", OptionsSource: "ai_models"},
			{Key: "instructions", Label: "Prompt do sistema", Type: "textarea", Placeholder: "Descreva como a IA deve se comportar..."},
			{Key: "additional_context", Label: "Contexto adicional", Type: "textarea", Placeholder: "Ex: O CEP {{last.cep}} fica no bairro {{last.bairro}}"},
			{Key: "context_window", Label: "Janela de contexto (mensagens)", Type: "number", Placeholder: "50"},
			{Key: "response_variable", Label: "Salvar resposta na variável", Type: "text", Placeholder: "ai_response"},
			{Key: "knowledge_base_ids", Label: "Bases de conhecimento", Type: "multi-select", Placeholder: "Selecione bases de conhecimento", OptionsSource: "knowledge_bases", Description: "Bases de conhecimento para RAG. Quando usando um agente, as bases vinculadas ao agente são usadas automaticamente."},
			{Key: "mcp_collection_ids", Label: "Coleções MCP", Type: "multi-select", Placeholder: "Selecione coleções MCP", OptionsSource: "mcp_collections", Description: "Coleções MCP que delimitam as ferramentas disponíveis para esta IA."},
			{Key: "custom_tools", Label: "Ferramentas", Type: "tools", Placeholder: ""},
		},
	}
}

func (e *aiAgentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	runID := ctx.Run.ID
	entryID := ctx.Run.EntryID
	entryType := shared.EntryType(ctx.Run.EntryType)
	logPrefix := fmt.Sprintf("[workflow][ai_agent][run:%s]", runID)

	responseMode, _ := ctx.Node.Config["response_mode"].(string)
	if responseMode == "" {
		responseMode = "default"
	}
	isWhatsApp := entryType == shared.EntryTypeWhatsApp
	isSegmented := responseMode == "segmented"

	var typingClient conversation.WhatsAppClient
	var latestWamid string
	if isWhatsApp && e.waSender != nil {
		if tc, wamid, err := e.resolveTypingClient(ctx); err == nil && tc != nil && wamid != "" {
			typingClient = tc
			latestWamid = wamid
			if err := tc.SendTypingIndicator(context.Background(), wamid); err != nil {
				log.Printf("%s typing indicator failed (pre-AI): %v", logPrefix, err)
			} else {
				log.Printf("%s typing indicator sent (pre-AI) wamid=%s", logPrefix, wamid)
			}
		} else if err != nil {
			log.Printf("%s could not resolve typing client: %v", logPrefix, err)
		}
	}

	source, _ := ctx.Node.Config["source"].(string)
	if source == "" {
		source = "agent"
	}

	var prompt, model, agentID string
	var resolvedAgent *agent.Agent

	if source == "prompt" {
		model, _ = ctx.Node.Config["model"].(string)
		prompt, _ = ctx.Node.Config["instructions"].(string)
		if model == "" || prompt == "" {
			log.Printf("%s ERROR: inline prompt mode but model or instructions missing", logPrefix)
			return nil, workflow.ErrNodeConfigMissing
		}
		log.Printf("%s using inline prompt mode, model=%s", logPrefix, model)
	} else {
		agentID, _ = ctx.Node.Config["agent_id"].(string)
		if agentID == "" {
			log.Printf("%s ERROR: agent mode but agent_id is empty", logPrefix)
			return nil, workflow.ErrNodeConfigMissing
		}

		ag, err := e.agentRepo.FindByID(agentID)
		if err != nil {
			log.Printf("%s ERROR: failed to load agent %s: %v", logPrefix, agentID, err)
			return nil, err
		}
		if ag == nil {
			log.Printf("%s ERROR: agent %s not found", logPrefix, agentID)
			return &workflow.NodeResult{Error: "agent not found: " + agentID}, nil
		}

		resolvedAgent = ag
		prompt = ag.MessagingPrompt
		if prompt == "" {
			prompt = "Sem instruções."
		}
		model = ag.MessagingModel
		if model == "" {
			log.Printf("%s ERROR: agent %s has no messaging model configured", logPrefix, agentID)
			return &workflow.NodeResult{Error: "agent has no messaging model"}, nil
		}
		agentID = ag.ID
		log.Printf("%s using agent=%s model=%s", logPrefix, agentID, model)
	}

	if resolvedAgent != nil {
		agentVars := make(map[string]string)
		if campvars, ok := ctx.State.Get("campvars"); ok {
			if m, ok := campvars.(map[string]interface{}); ok {
				agentVars = agent.MetadataToVars(m)
			}
		}
		interpolated := agent.InterpolateAgent(resolvedAgent, agentVars)
		if interpolated.MessagingPrompt != "" {
			prompt = interpolated.MessagingPrompt
		}
	}
	prompt = workflow.Interpolate(prompt, ctx.State, nil)

	inputMode, _ := ctx.Node.Config["input_mode"].(string)
	if inputMode == "" {
		inputMode = "conversation"
	}

	contextWindow := aiAgentDefaultContextWindow
	if cw, ok := ctx.Node.Config["context_window"].(float64); ok && cw > 0 {
		contextWindow = int(cw)
	}

	var messages []ai.Message

	switch inputMode {
	case "custom":

		textTemplate, _ := ctx.Node.Config["text"].(string)
		if textTemplate == "" {
			textTemplate = "{{message}}"
		}
		userMessage := workflow.Interpolate(textTemplate, ctx.State, nil)
		if userMessage == "" {
			log.Printf("%s ERROR: custom input mode but interpolated text is empty (template=%q)", logPrefix, textTemplate)
			return &workflow.NodeResult{Error: "custom input text resolved to empty"}, nil
		}

		includeHistory, _ := ctx.Node.Config["include_history"].(string)
		if includeHistory == "yes" {
			history, err := e.messageRepo.ListByEntry(entryID, entryType)
			if err != nil {
				log.Printf("%s ERROR: failed to load history for custom mode: %v", logPrefix, err)
				return nil, err
			}
			messages = composeHistory(history, contextWindow)
			log.Printf("%s custom mode: loaded %d history messages + custom text", logPrefix, len(messages))
		}

		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: userMessage})
		log.Printf("%s custom input mode: text=%d chars, include_history=%s",
			logPrefix, len(userMessage), includeHistory)

	default:
		history, err := e.messageRepo.ListByEntry(entryID, entryType)
		if err != nil {
			log.Printf("%s ERROR: failed to load conversation history for entry %s: %v", logPrefix, entryID, err)
			return nil, err
		}
		messages = composeHistory(history, contextWindow)
		log.Printf("%s loaded %d conversation messages (from %d raw, window=%d) for entry=%s",
			logPrefix, len(messages), len(history), contextWindow, entryID)
	}

	if len(messages) == 0 {
		fallback := ctx.State.GetString("message")
		if fallback == "" {
			fallback = workflow.Interpolate("{{last.text}}", ctx.State, nil)
		}
		if fallback != "" {
			messages = append(messages, ai.Message{Role: ai.RoleUser, Content: fallback})
			log.Printf("%s conversation history empty, using state fallback (%d chars)", logPrefix, len(fallback))
		}
	}

	if raw, ok := ctx.State.Get("_pending_tool_call"); ok {
		consumed := false
		if pending, ok := raw.(map[string]interface{}); ok {
			toolName, _ := pending["name"].(string)
			sourceAI, _ := pending["source_ai_node"].(string)
			targetNode, _ := pending["target_node"].(string)

			inject := toolName != "" && sourceAI == ctx.Node.ID

			if inject {
				if prevID := ctx.State.GetString("_prev_node_id"); prevID != "" {
					if prevNode := ctx.Graph.FindNode(prevID); prevNode != nil {
						if prevNode.Type.IsWait() || prevNode.Type.IsTrigger() {
							inject = false
						}
					}
				}
			}

			if inject {
				var toolResult map[string]interface{}
				if targetNode != "" {
					toolResult = collectNodeOutput(ctx.State, targetNode)
				}
				if len(toolResult) == 0 {

					toolResult = collectLastOutput(ctx.State)
				}
				resultJSON, _ := json.Marshal(toolResult)

				summary := fmt.Sprintf("[Resultado da ferramenta %s]: %s", toolName, string(resultJSON))
				messages = append(messages, ai.Message{
					Role:    ai.RoleUser,
					Content: summary,
				})
				consumed = true

				log.Printf("%s injected tool result from %q (target=%s) as user message (%d chars)",
					logPrefix, toolName, targetNode, len(summary))

				var existing []interface{}
				if rawActions, ok := ctx.State.Get("_executed_actions"); ok {
					if arr, ok := rawActions.([]interface{}); ok {
						existing = arr
					}
				}
				entry := fmt.Sprintf("Resultado de %s: %s", toolName, truncate(string(resultJSON), 300))
				existing = append(existing, entry)
				ctx.State.Set("_executed_actions", existing)
			}
		}

		ctx.State.Delete("_pending_tool_call")
		if !consumed {
			log.Printf("%s cleared stale _pending_tool_call (previous node was wait/trigger or different AI)", logPrefix)
		}
	}

	if extra, ok := ctx.Node.Config["additional_context"].(string); ok && extra != "" {
		interpolated := workflow.Interpolate(extra, ctx.State, nil)
		prompt = prompt + "\n\n" + interpolated
		log.Printf("%s appended additional_context (%d chars after interpolation)", logPrefix, len(interpolated))
	}

	if e.ragService != nil {

		var userQuery string
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == ai.RoleUser {
				userQuery = messages[i].Content
				break
			}
		}
		if userQuery != "" {
			var ragCtx string
			if resolvedAgent != nil && resolvedAgent.IsRAGEnabled() {
				ragCtx = rag_usecase.BuildRAGContext(context.Background(), e.ragService, resolvedAgent, userQuery)
				if ragCtx != "" {
					log.Printf("%s RAG (agent mode): injected %d chars from agent %s", logPrefix, len(ragCtx), resolvedAgent.ID)
				}
			} else if rawKBIDs, ok := ctx.Node.Config["knowledge_base_ids"]; ok {
				kbIDs := parseStringSlice(rawKBIDs)
				if len(kbIDs) > 0 {
					result, err := e.ragService.Query(context.Background(), rag.QueryInput{
						KnowledgeBaseIDs: kbIDs,
						Query:            userQuery,
						TopK:             10,
						MinScore:         0.3,
						IncludeMetadata:  true,
					})
					if err != nil {
						log.Printf("%s RAG (prompt mode): query failed: %v", logPrefix, err)
					} else if len(result.Results) > 0 {
						ragCtx = formatRAGResults(result)
						log.Printf("%s RAG (prompt mode): injected %d chars from %d KBs", logPrefix, len(ragCtx), len(kbIDs))
					}
				}
			}
			if ragCtx != "" {
				prompt += ragCtx
			}
		}
	}

	if raw, ok := ctx.State.Get("_executed_actions"); ok {
		if actions, ok := raw.([]interface{}); ok && len(actions) > 0 {
			var sb strings.Builder
			sb.WriteString("\n\n[AÇÕES JÁ EXECUTADAS NESTA CONVERSA — NÃO repita a menos que o usuário peça explicitamente]\n")
			for _, a := range actions {
				if s, ok := a.(string); ok {
					sb.WriteString("• ")
					sb.WriteString(s)
					sb.WriteString("\n")
				}
			}
			prompt += sb.String()
			log.Printf("%s injected contextual update: %d previous actions", logPrefix, len(actions))
		}
	}

	customTools := parseCustomToolsConfig(ctx.Node.Config)
	hasCustomTools := len(customTools) > 0

	var toolDefs []tools.Definition
	var toolChoice string

	if hasCustomTools {
		toolDefs = buildCustomToolDefs(customTools)

		edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
		hasDefaultEdge := resolveEdgeByLabelStrict(edges, "default") != ""
		if hasDefaultEdge || isSegmented {
			toolChoice = "auto"
		} else {
			toolChoice = "required"
		}
		log.Printf("%s configured %d custom tools, tool_choice=%s (default_edge=%v)", logPrefix, len(toolDefs), toolChoice, hasDefaultEdge)
		for _, td := range toolDefs {
			log.Printf("%s   tool: %s — %s", logPrefix, td.Name, td.Description)
		}
	}

	const minBalanceFloor int64 = 10_000
	if e.cachedBalanceChecker != nil {
		bal, err := e.cachedBalanceChecker.GetBalance(ctx.Workflow.WorkspaceID)
		if err != nil {
			log.Printf("%s balance check error for workspace %s: %v — blocking AI call (fail-closed)", logPrefix, ctx.Workflow.WorkspaceID, err)
			return nil, fmt.Errorf("balance check failed: %w", err)
		}
		if bal < minBalanceFloor {
			log.Printf("%s workspace %s balance (%d micros) below minimum floor (%d micros), blocking AI call", logPrefix, ctx.Workflow.WorkspaceID, bal, minBalanceFloor)

			edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
			errorNodeID := resolveEdgeByLabelStrict(edges, "erro")
			if errorNodeID != "" {
				return &workflow.NodeResult{
					NextNodeID: errorNodeID,
					Output: map[string]interface{}{
						"response_text": "",
						"error":         "insufficient balance",
						"agent_id":      agentID,
						"model":         model,
					},
				}, nil
			}
			return nil, fmt.Errorf("insufficient balance for AI call (workspace %s)", ctx.Workflow.WorkspaceID)
		}
	}

	log.Printf("%s >>> calling AI: model=%s messages=%d tools=%d tool_choice=%s segmented=%v",
		logPrefix, model, len(messages), len(toolDefs), toolChoice, isSegmented)

	callStart := time.Now()
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), aiAgentTimeout)
	defer cancel()

	output, err := e.aiService.Generate(ctxWithTimeout, ai.GenerateInput{
		WorkspaceID:       ctx.Workflow.WorkspaceID,
		Model:             model,
		SystemPrompt:      prompt,
		Messages:          messages,
		Temperature:       0.2,
		Tools:             toolDefs,
		ToolExecutionMode: ai.ToolExecutionModeNone,
		ToolChoice:        toolChoice,
		MaxTokens:         2000,
		SegmentedResponse: isSegmented,
	})

	elapsed := time.Since(callStart)

	if err != nil {
		log.Printf("%s <<< AI FAILED after %s: %v", logPrefix, elapsed.Round(time.Millisecond), err)

		edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
		errorNodeID := resolveEdgeByLabelStrict(edges, "erro")
		if errorNodeID != "" {
			log.Printf("%s routing to error edge node %s", logPrefix, errorNodeID)
			return &workflow.NodeResult{
				NextNodeID: errorNodeID,
				Output: map[string]interface{}{
					"response_text": "",
					"error":         err.Error(),
					"agent_id":      agentID,
					"model":         model,
				},
			}, nil
		}
		return nil, fmt.Errorf("ai generate failed after %s: %w", elapsed.Round(time.Millisecond), err)
	}

	responseText := strings.TrimSpace(output.Message.Content)
	if responseText == "" && len(output.Messages) > 0 {
		responseText = strings.TrimSpace(output.Messages[len(output.Messages)-1])
	}

	log.Printf("%s <<< AI OK in %s: response=%d chars, tool_calls=%d",
		logPrefix, elapsed.Round(time.Millisecond), len(responseText), len(output.ToolCalls))

	if len(output.ToolCalls) > 0 || responseText != "" {
		var existing []interface{}
		if raw, ok := ctx.State.Get("_executed_actions"); ok {
			if arr, ok := raw.([]interface{}); ok {
				existing = arr
			}
		}
		for _, tc := range output.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			entry := fmt.Sprintf("Ferramenta chamada: %s(%s)", tc.Name, string(argsJSON))
			existing = append(existing, entry)
			log.Printf("%s contextual update: recorded action %s", logPrefix, entry)
		}
		if responseText != "" && len(output.ToolCalls) == 0 {
			entry := fmt.Sprintf("Resposta enviada: %s", truncate(responseText, 150))
			existing = append(existing, entry)
		}

		const maxActions = 20
		if len(existing) > maxActions {
			existing = existing[len(existing)-maxActions:]
		}
		ctx.State.Set("_executed_actions", existing)
	}

	responseVar, _ := ctx.Node.Config["response_variable"].(string)
	if responseVar == "" {
		responseVar = "ai_response"
	}

	var segmentedMessages []string
	if isSegmented && responseText != "" {
		segmentedMessages = splitIntoSegments(responseText)
	}

	result := map[string]interface{}{
		"response_text": responseText,
		"agent_id":      agentID,
		"model":         model,
		"delivered":     false,
	}
	if len(segmentedMessages) > 0 {

		iface := make([]interface{}, len(segmentedMessages))
		for i, m := range segmentedMessages {
			iface[i] = m
		}
		result["messages"] = iface
	}

	// Track AI-as-attendant whenever the node produced a reply (even if tool-routed).
	if responseText != "" || len(output.ToolCalls) > 0 {
		e.recordWorkflowAIAttendance(ctx, agentID, model, "")
	}

	if isSegmented && isWhatsApp && e.waSender != nil && len(segmentedMessages) > 0 && len(output.ToolCalls) == 0 {
		log.Printf("%s segmented WhatsApp delivery: %d messages", logPrefix, len(segmentedMessages))
		allSent := true
		for i, msgText := range segmentedMessages {

			if i > 0 && typingClient != nil && latestWamid != "" {
				time.Sleep(segmentedTypingDelay)
				if err := typingClient.SendTypingIndicator(context.Background(), latestWamid); err != nil {
					log.Printf("%s typing indicator failed (segment %d): %v", logPrefix, i, err)
				}
				time.Sleep(segmentedTypingMinShow)
			} else if i == 0 {

				time.Sleep(segmentedTypingMinShow)
			}

			sendOutput, _, sendErr := e.waSender.SendText(context.Background(), ctx.Run, msgText, conversation.MessageTypeAIResponse)
			if sendErr != nil {
				log.Printf("%s segmented send failed at segment %d/%d: %v", logPrefix, i+1, len(segmentedMessages), sendErr)
				allSent = false
				break
			}
			wamid := ""
			if sendOutput != nil {
				wamid = sendOutput.MessageID
			}
			log.Printf("%s segmented send %d/%d: wamid=%s text=%d chars", logPrefix, i+1, len(segmentedMessages), wamid, len(msgText))
		}
		if allSent {
			result["delivered"] = true
			ctx.State.Set("_ai_agent_delivered", true)
			log.Printf("%s all %d segments delivered via WhatsApp", logPrefix, len(segmentedMessages))
		}
	}

	// Default (non-segmented) delivery goes through the channel sender, so the
	// agent node answers on every adapter-backed channel — not only WhatsApp.
	// Segmented delivery above stays WhatsApp-only: it paces several sends
	// against WhatsApp's own rate rules.
	if !isSegmented && responseText != "" && len(output.ToolCalls) == 0 && e.sender.Supports(ctx.Run) {
		sent, sendErr := e.sender.SendText(context.Background(), ctx.Run, responseText, conversation.MessageTypeAIResponse)
		if sendErr != nil {
			log.Printf("%s default delivery failed: %v", logPrefix, sendErr)
		} else if sent == nil {
			// The channel declined (a closed outbound window is the usual cause).
			// The response is still recorded by the node; it simply was not sent.
			log.Printf("%s default delivery withheld by channel %q", logPrefix, ctx.Run.EntryType)
		} else {
			result["delivered"] = true
			ctx.State.Set("_ai_agent_delivered", true)
			log.Printf("%s default delivery: channel=%s id=%s text=%d chars",
				logPrefix, ctx.Run.EntryType, sent.ProviderMessageID, len(responseText))
		}
	}

	if hasCustomTools && len(output.ToolCalls) > 0 {
		tc := output.ToolCalls[0]
		log.Printf("%s AI chose tool %q with args %v → routing via edge label", logPrefix, tc.Name, tc.Arguments)

		result["tool_name"] = tc.Name
		result["tool_args"] = tc.Arguments
		for k, v := range tc.Arguments {
			result[k] = v
		}

		edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
		nextNodeID := resolveEdgeByLabel(edges, tc.Name)
		if nextNodeID == "" {
			log.Printf("%s WARNING: no edge found for tool %q, available edges: %v", logPrefix, tc.Name, edgeLabels(edges))
		} else {
			log.Printf("%s routing to node %s via tool edge %q", logPrefix, nextNodeID, tc.Name)
		}

		ctx.State.Set("_pending_tool_call", map[string]interface{}{
			"name":           tc.Name,
			"args":           tc.Arguments,
			"source_ai_node": ctx.Node.ID,
			"target_node":    nextNodeID,
		})

		persistAIState(ctx.State, responseVar, result)

		return &workflow.NodeResult{
			NextNodeID: nextNodeID,
			Output:     result,
		}, nil
	}

	if responseText != "" {
		persistAIState(ctx.State, responseVar, result)
	}

	if hasCustomTools {
		log.Printf("%s WARNING: AI did NOT call any tool (response: %q)", logPrefix, truncate(responseText, 200))

		edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
		nextNodeID := resolveEdgeByLabelStrict(edges, "default")
		if nextNodeID == "" {
			if isSegmented {
				log.Printf("%s segmented mode responded but no default edge — completing", logPrefix)
				return &workflow.NodeResult{
					Output:   result,
					Complete: true,
				}, nil
			}
			log.Printf("%s ERROR: no default edge and no tool called — dead end", logPrefix)
			return &workflow.NodeResult{
				Error: "AI agent has custom tools but model did not call any tool and no 'Padrão' (default) output is connected",
			}, nil
		}
		log.Printf("%s routing to default node %s", logPrefix, nextNodeID)
		return &workflow.NodeResult{
			NextNodeID: nextNodeID,
			Output:     result,
		}, nil
	}

	log.Printf("%s completed: response=%d chars saved to var=%q", logPrefix, len(responseText), responseVar)
	// Route the text response through the "default" handle. Empty (legacy graphs
	// with an unlabeled continuation edge) falls back to the engine's first-edge
	// behavior, so older workflows keep working.
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(edges, "default"),
		Output:     result,
	}, nil
}

func composeHistory(history []*conversation.Message, limit int) []ai.Message {
	if len(history) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = aiAgentDefaultContextWindow
	}

	start := 0
	if len(history) > limit {
		start = len(history) - limit
	}

	messages := make([]ai.Message, 0, len(history[start:]))
	for _, msg := range history[start:] {
		if msg == nil {
			continue
		}
		switch msg.MessageType {
		case conversation.MessageTypeToolCall,
			conversation.MessageTypeToolResult,
			conversation.MessageTypeSystem:
			continue
		}
		if msg.MessageType.IsCallEvent() {
			continue
		}
		content := strings.TrimSpace(msg.Text)
		if content == "" {
			continue
		}

		if len(msg.Metadata) > 0 {
			var meta map[string]string
			if err := json.Unmarshal(msg.Metadata, &meta); err == nil {
				if et := strings.TrimSpace(meta["extracted_text"]); et != "" {
					content = content + "\n\n" + et
				}
			}
		}

		role := ai.RoleUser
		if msg.MessageType == conversation.MessageTypeAIResponse ||
			msg.MessageType == conversation.MessageTypeTemplate {
			role = ai.RoleAssistant
		}

		messages = append(messages, ai.Message{Role: role, Content: content})
	}
	return messages
}

type customToolConfig struct {
	Name        string
	Description string
	Parameters  []customToolParam
}

type customToolParam struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Enum        []string
}

func parseCustomToolsConfig(config map[string]interface{}) []customToolConfig {
	raw, ok := config["custom_tools"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	var out []customToolConfig
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := m["description"].(string)
		ct := customToolConfig{
			Name:        name,
			Description: desc,
		}
		if params, ok := m["parameters"].([]interface{}); ok {
			for _, p := range params {
				pm, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				pName, _ := pm["name"].(string)
				if pName == "" {
					continue
				}
				pType, _ := pm["type"].(string)
				if pType == "" {
					pType = "string"
				}
				pDesc, _ := pm["description"].(string)
				pReq, _ := pm["required"].(bool)
				var pEnum []string
				if rawEnum, ok := pm["enum"].([]interface{}); ok {
					for _, e := range rawEnum {
						if s, ok := e.(string); ok && s != "" {
							pEnum = append(pEnum, s)
						}
					}
				}
				ct.Parameters = append(ct.Parameters, customToolParam{
					Name:        pName,
					Type:        pType,
					Description: pDesc,
					Required:    pReq,
					Enum:        pEnum,
				})
			}
		}
		out = append(out, ct)
	}
	return out
}

func buildCustomToolDefs(cts []customToolConfig) []tools.Definition {
	defs := make([]tools.Definition, 0, len(cts))
	for _, ct := range cts {
		params := make(map[string]tools.Parameter, len(ct.Parameters))
		var required []string
		for _, p := range ct.Parameters {
			params[p.Name] = tools.Parameter{
				Type:        p.Type,
				Description: p.Description,
				Enum:        p.Enum,
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		defs = append(defs, tools.Definition{
			Name:        ct.Name,
			Description: ct.Description,
			Parameters:  params,
			Required:    required,
		})
	}
	return defs
}

// AIAgentToolOutputs is the single backend authority for an AI-agent node's
// output handles. It ALWAYS declares the response and error paths, plus one route
// per custom tool — so every ai_agent node (with or without tools) has the same,
// backend-defined handle set. The frontend renders this verbatim; it never
// recomputes handles or their optional flags.
func AIAgentToolOutputs(config map[string]interface{}) []workflow.HandleDefinition {
	cts := parseCustomToolsConfig(config)
	outputs := make([]workflow.HandleDefinition, 0, len(cts)+2)
	for _, ct := range cts {
		if strings.TrimSpace(ct.Name) == "" {
			continue
		}
		outputs = append(outputs, workflow.HandleDefinition{
			ID:    ct.Name,
			Label: ct.Name,
		})
	}
	// The response path is REQUIRED: whenever the model answers with text instead
	// of calling a tool (it always may, including in segmented mode), the flow
	// leaves through "default". Forcing it to be connected stops a text reply from
	// silently dead-ending — the builder must wire it (e.g. to wait_for_reply to
	// continue the conversation, or to end).
	outputs = append(outputs, workflow.HandleDefinition{
		ID:    "default",
		Label: "Resposta (texto)",
	})
	outputs = append(outputs, workflow.HandleDefinition{
		ID:       "erro",
		Label:    "Erro",
		Optional: true,
	})
	return outputs
}

const (
	segmentMaxLen = 200

	segmentMergeMin = 30

	segmentMaxCount = 6
)

func splitIntoSegments(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	paragraphs := splitParagraphs(text)

	var segments []string
	for _, p := range paragraphs {
		if len(p) <= segmentMaxLen {
			segments = append(segments, p)
		} else {
			segments = append(segments, splitSentences(p)...)
		}
	}

	segments = mergeShortSegments(segments, segmentMergeMin)

	if len(segments) > segmentMaxCount {

		merged := strings.Join(segments[segmentMaxCount-1:], "\n\n")
		segments = append(segments[:segmentMaxCount-1], merged)
	}

	return segments
}

func splitParagraphs(text string) []string {

	text = strings.ReplaceAll(text, "\r\n", "\n")
	raw := strings.Split(text, "\n\n")
	var out []string
	for _, r := range raw {
		if t := strings.TrimSpace(r); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	return out
}

func splitSentences(text string) []string {
	var segments []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		if runes[i] == '.' || runes[i] == '?' || runes[i] == '!' {

			atEnd := i == len(runes)-1
			nextIsSpace := !atEnd && (runes[i+1] == ' ' || runes[i+1] == '\n')

			if atEnd || nextIsSpace {
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					segments = append(segments, seg)
				}
				current.Reset()

				if nextIsSpace && !atEnd {
					i++
				}
			}
		}
	}

	if rest := strings.TrimSpace(current.String()); rest != "" {
		segments = append(segments, rest)
	}

	if len(segments) == 0 {
		return []string{text}
	}

	var merged []string
	var buf strings.Builder
	for _, s := range segments {
		if buf.Len() > 0 && buf.Len()+1+len(s) > segmentMaxLen {
			merged = append(merged, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(s)
	}
	if buf.Len() > 0 {
		merged = append(merged, strings.TrimSpace(buf.String()))
	}
	return merged
}

func mergeShortSegments(segments []string, minLen int) []string {
	if len(segments) <= 1 {
		return segments
	}
	var out []string
	var carry string
	for _, s := range segments {
		if carry != "" {
			s = carry + "\n\n" + s
			carry = ""
		}
		if len(s) < minLen {
			carry = s
			continue
		}
		out = append(out, s)
	}

	if carry != "" {
		if len(out) > 0 {
			out[len(out)-1] += "\n\n" + carry
		} else {
			out = append(out, carry)
		}
	}
	return out
}

// resolveTypingClient is WhatsApp-only: the typing indicator is addressed by a
// wamid, which no other channel has.
func (e *aiAgentExecutor) resolveTypingClient(ctx *workflow.NodeContext) (conversation.WhatsAppClient, string, error) {
	if e.waSender == nil || ctx.Run == nil {
		return nil, "", nil
	}
	if shared.EntryType(ctx.Run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}

	target, err := e.waSender.resolveTarget(ctx.Run.EntryID, ctx.Run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	client, _, err := e.waSender.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, "", err
	}

	if e.messageRepo == nil {
		return nil, "", nil
	}
	messages, err := e.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   ctx.Run.EntryID,
		EntryType: shared.EntryTypeWhatsApp,
		Limit:     100,
	})
	if err != nil {
		return nil, "", err
	}
	for _, msg := range messages {
		if msg == nil || !msg.MessageType.IsInbound() || msg.WhatsAppMessageID == nil {
			continue
		}
		wamid := strings.TrimSpace(*msg.WhatsAppMessageID)
		if wamid != "" {
			return client, wamid, nil
		}
	}

	return client, "", nil
}

func edgeLabels(edges []workflow.Edge) []string {
	labels := make([]string, len(edges))
	for i, e := range edges {
		if e.Label != "" {
			labels[i] = e.Label
		} else {
			labels[i] = "(unlabeled)"
		}
	}
	return labels
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func persistAIState(state *workflow.RunState, responseVar string, result map[string]interface{}) {
	if state == nil || result == nil {
		return
	}
	if responseVar != "" {
		state.Set(responseVar, result)
	}
	for key, value := range result {
		state.Set("_ai_"+key, value)
	}
	if responseText, ok := result["response_text"]; ok {
		state.Set("_ai_response", responseText)
	}
}

func collectLastOutput(state *workflow.RunState) map[string]interface{} {
	result := make(map[string]interface{})
	if state == nil || state.Vars == nil {
		return result
	}
	for k, v := range state.Vars {
		if strings.HasPrefix(k, "_last_") {
			key := strings.TrimPrefix(k, "_last_")
			result[key] = v
		}
	}
	return result
}

func collectNodeOutput(state *workflow.RunState, nodeID string) map[string]interface{} {
	result := make(map[string]interface{})
	if state == nil || state.Vars == nil || nodeID == "" {
		return result
	}
	prefix := "_node_" + nodeID + "_"
	for k, v := range state.Vars {
		if strings.HasPrefix(k, prefix) {
			key := strings.TrimPrefix(k, prefix)
			result[key] = v
		}
	}
	return result
}

func parseStringSlice(val interface{}) []string {
	switch v := val.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func formatRAGResults(result *rag.QueryOutput) string {
	if result == nil || len(result.Results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n--- Base de Conhecimento (Informações Recuperadas) ---\n")
	sb.WriteString("REGRAS DE FUNDAMENTAÇÃO (OBRIGATÓRIO — VIOLAÇÃO = FALHA CRÍTICA):\n")
	sb.WriteString("1. Use SOMENTE as informações abaixo para responder perguntas factuais. NUNCA invente, extrapole ou preencha lacunas.\n")
	sb.WriteString("2. Se a informação necessária NÃO está abaixo, diga: 'Não tenho essa informação disponível na minha base de dados'.\n")
	sb.WriteString("3. NUNCA fabrique preços, endereços, nomes, datas, grades ou qualquer dado específico que NÃO esteja nos trechos abaixo.\n")
	sb.WriteString("4. Se a informação é parcial, apresente APENAS o que foi encontrado e indique o que NÃO está disponível.\n")
	sb.WriteString("5. Prefira SEMPRE dizer 'não sei' a inventar qualquer dado.\n\n")
	for i, r := range result.Results {
		sb.WriteString(fmt.Sprintf("[%d] (Fonte: %s, relevância: %.0f%%)\n%s\n\n", i+1, r.DocumentName, r.Score*100, r.Content))
	}
	sb.WriteString("--- Fim da Base de Conhecimento ---\n\n")
	return sb.String()
}
