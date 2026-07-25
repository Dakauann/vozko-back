package tools_usecase

import (
	"context"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/tools"
)

const FinishConversationToolName = "finish_conversation"

type finishConversationTool struct {
	status conversation.ConversationStatusUpdater
	hub    conversation.EventBroadcaster
}

// NewFinishConversationToolUseCase lets the AI finish a conversation with
// close_source=ai. All status mutations go through ConversationStatusService.
func NewFinishConversationToolUseCase(status conversation.ConversationStatusUpdater, hub conversation.EventBroadcaster) tools.Handler {
	if status == nil {
		return nil
	}
	return &finishConversationTool{status: status, hub: hub}
}

func (t *finishConversationTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               FinishConversationToolName,
		DisplayName:        "Finalizar conversa",
		DisplayDescription: "Encerra a conversa quando o atendimento foi concluído (cliente resolvido ou se despediu).",
		Description: `Finaliza a conversa atual (status finished) quando o atendimento está concluído.

QUANDO USAR:
- O cliente confirmou que o problema foi resolvido
- O cliente se despediu e não há mais pendências
- Você concluiu o fluxo e não há mais ações necessárias

QUANDO NÃO USAR:
- Ainda espera resposta do cliente
- Transferiu ou vai transferir para humano
- Pediu dados e ainda não recebeu

Após finalizar, se o cliente mandar mensagem de novo a conversa reabre automaticamente.`,
		Parameters: map[string]tools.Parameter{
			"reason": {
				Type:               "string",
				Description:        "Breve motivo em português (ex: cliente confirmou resolução).",
				DisplayName:        "Motivo",
				DisplayDescription: "Por que a conversa foi finalizada",
			},
		},
		Required:   []string{},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging, tools.VisibilityPostConversation},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *finishConversationTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *finishConversationTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	_ = ctx
	entryID, _ := config["__entry_id"].(string)
	entryType, _ := config["__entry_type"].(string)
	entryID = strings.TrimSpace(entryID)
	entryType = strings.TrimSpace(entryType)
	if entryID == "" || entryType == "" {
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento.",
			IsError: true,
		}, nil
	}
	if entryType != "whatsapp" && entryType != "voice" {
		return tools.ExecutionResult{
			Result:  "Tipo de conversa não suportado para finalização.",
			IsError: true,
		}, nil
	}

	if err := t.status.Finish(entryID, entryType, conversation.FinishOptions{
		Source: conversation.CloseSourceAI,
		Reason: conversation.CloseReasonAIResolved,
	}); err != nil {
		log.Printf("[FinishConversation] entry=%s type=%s err=%v", entryID, entryType, err)
		return tools.ExecutionResult{
			Result:  "Falha ao finalizar a conversa. Tente novamente.",
			IsError: true,
		}, nil
	}

	if t.hub != nil {
		// Best-effort live UI update; status service already persisted.
		if broadcaster, ok := t.hub.(interface {
			BroadcastConversationStatus(entryID, entryType, status string)
		}); ok {
			broadcaster.BroadcastConversationStatus(entryID, entryType, string(conversation.ConversationStatusFinished))
		}
	}

	reason, _ := params["reason"].(string)
	reason = strings.TrimSpace(reason)
	msg := "Conversa finalizada com sucesso (status finished, origem IA)."
	if reason != "" {
		msg = "Conversa finalizada: " + reason
	}
	return tools.ExecutionResult{Result: msg}, nil
}

var _ tools.Handler = (*finishConversationTool)(nil)
