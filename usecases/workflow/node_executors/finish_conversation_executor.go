package node_executors

import (
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// finishConversationExecutor marks the CRM conversation finished via the
// shared ConversationStatusService (close_source=system, reason=workflow).
// It does not end the workflow run, connect "sucesso" to an end node if needed.
type finishConversationExecutor struct {
	status conversation.ConversationStatusUpdater
}

func NewFinishConversationExecutor(status conversation.ConversationStatusUpdater) workflow.NodeExecutor {
	return &finishConversationExecutor{status: status}
}

func (e *finishConversationExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionFinishConversation,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Finalizar Conversa",
		Description: "Encerra a conversa atual no CRM (status Finalizada). Reabre se o cliente mandar mensagem de novo.",
		Icon:        "CheckCircle",
		Guidance: workflow.NodeGuidance{
			When: "Quando o fluxo concluiu o atendimento e a conversa deve sair do conjunto aberto (ex.: após despedida, resolução, ou fim de automação).",
			Behavior: "Chama o mesmo serviço de status que humanos e a ferramenta finish_conversation da IA. " +
				"Origem: system · motivo: workflow. Idempotente se já estiver finalizada. " +
				"Não encerra o run do workflow, conecte a saída sucesso a um nó Fim se quiser terminar o fluxo.",
			Examples: []string{
				"IA responde → Finalizar conversa → Fim",
				"Enviar texto de despedida → Finalizar conversa → Fim",
			},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a conversa foi finalizada (ou já estava)"},
			{Key: "entry_id", Description: "ID da entrada finalizada"},
			{Key: "entry_type", Description: "Tipo da entrada (whatsapp|voice)"},
			{Key: "close_source", Description: "Proveniência do encerramento (system)"},
			{Key: "close_reason", Description: "Motivo (workflow)"},
			{Key: "error", Description: "Descrição do erro quando falha"},
		},
		DefaultConfig: map[string]interface{}{
			"note": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{
				Key:         "note",
				Label:       "Nota (opcional)",
				Type:        "text",
				Placeholder: "ex.: fluxo de pós-venda concluído",
				Description: "Apenas registro no output do nó; o motivo de sistema permanece workflow.",
			},
		},
	}
}

func (e *finishConversationExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	if e.status == nil {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   "serviço de status de conversa não configurado",
			},
		}, nil
	}

	entryID := strings.TrimSpace(ctx.Run.EntryID)
	entryType := strings.TrimSpace(ctx.Run.EntryType)
	if entryID == "" || entryType == "" {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   "run sem entry_id/entry_type, finalize só em fluxos de conversa",
			},
		}, nil
	}
	// Same predicate the AI finish tool uses: a workflow that can transfer and
	// assign a conversation must also be able to close it, on every channel.
	if !shared.EntryType(entryType).SupportsConversationClosing() {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("tipo de entrada %q não suporta finalização", entryType),
			},
		}, nil
	}

	note, _ := ctx.Node.Config["note"].(string)
	note = strings.TrimSpace(workflow.Interpolate(note, ctx.State, nil))

	if err := e.status.Finish(entryID, entryType, conversation.FinishOptions{
		Source: conversation.CloseSourceSystem,
		Reason: conversation.CloseReasonWorkflow,
	}); err != nil {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success":    false,
				"entry_id":   entryID,
				"entry_type": entryType,
				"error":      fmt.Sprintf("falha ao finalizar conversa: %v", err),
			},
		}, nil
	}

	out := map[string]interface{}{
		"success":      true,
		"entry_id":     entryID,
		"entry_type":   entryType,
		"close_source": string(conversation.CloseSourceSystem),
		"close_reason": string(conversation.CloseReasonWorkflow),
	}
	if note != "" {
		out["note"] = note
	}

	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
		Output:     out,
	}, nil
}
