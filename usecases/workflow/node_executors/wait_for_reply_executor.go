package node_executors

import (
	"time"
	"vozko/domain/workflow"
)

type waitForReplyExecutor struct{}

func NewWaitForReplyExecutor() workflow.NodeExecutor {
	return &waitForReplyExecutor{}
}

func (e *waitForReplyExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeWaitForReply,
		Category:    workflow.NodeCategoryWait,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Aguardar Resposta",
		Description: "Pausa o fluxo até o contato responder ou expirar o tempo.",
		Icon:        "ChatTeardropDots",
		Guidance: workflow.NodeGuidance{
			When:     "Para pausar e aguardar a próxima mensagem do contato (essencial para conversa contínua).",
			Behavior: "Pausa o fluxo até o contato responder. Padrão conversacional: conecte 'replied' de volta ao agente e 'timeout' para encerrar; o ciclo é válido porque o wait garante término.",
			Examples: []string{
				"Padrão conversacional: ai_agent → wait_for_reply; aresta \"replied\" → de volta ao ai_agent; aresta \"timeout\" → end.",
			},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "replied", Label: "Respondeu"},
			{ID: "timeout", Label: "Não respondeu", Optional: true},
		},
		DefaultConfig: map[string]interface{}{
			"timeout_seconds": float64(300),
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "timeout_seconds", Label: "Timeout (segundos)", Type: "number", Placeholder: "300", Required: true},
		},
	}
}

func (e *waitForReplyExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	timeoutSeconds, _ := ctx.Node.Config["timeout_seconds"].(float64)
	if timeoutSeconds <= 0 {

		if timeoutMinutes, ok := ctx.Node.Config["timeout_minutes"].(float64); ok && timeoutMinutes > 0 {
			timeoutSeconds = timeoutMinutes * 60
		}
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	wakeAt := time.Now().UTC().Add(time.Duration(timeoutSeconds * float64(time.Second))).UnixMilli()
	return &workflow.NodeResult{
		Wait: &workflow.WaitInstruction{
			WakeAt: wakeAt,
			Reason: workflow.WaitReasonReply,
		},
	}, nil
}
