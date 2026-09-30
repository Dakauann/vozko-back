package node_executors

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/workflow"
)

const queueTimeoutHandle = "timeout"

var errQueueRequired = errors.New("choose the queue that answers this call")

type transferToQueueExecutor struct{}

func NewTransferToQueueExecutor() workflow.NodeExecutor {
	return &transferToQueueExecutor{}
}

func (e *transferToQueueExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeTransferToQueue,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeVoice},
		Label:       "Transferir para Fila",
		Description: "Coloca quem ligou na fila com música de espera até um atendente livre atender.",
		Icon:        "Headset",
		Guidance: workflow.NodeGuidance{
			When: "Para passar a ligação a uma pessoa: depois do menu, na opção 'falar com atendente'. Só existe em fluxos de voz.",
			Behavior: "Escolha a fila (queue_id, use find_resource call_queues). Quem ligou ouve a música de espera da fila e é entregue " +
				"a quem está livre há mais tempo (ou à estratégia da fila). 'notes' chega ao atendente antes de atender e aceita " +
				"variáveis como {{var.x}}. Quando alguém atende, o fluxo termina e a ligação segue com o atendente. Se o tempo " +
				"máximo de espera da fila passar, segue pela saída 'timeout'; sem ela conectada, a ligação é encerrada.",
			Examples: []string{
				"config: {\"queue_id\":\"<id>\",\"notes\":\"Escolheu suporte no menu\"}  // aresta \"timeout\" para um áudio de desculpas",
			},
		},
		DefaultConfig: map[string]interface{}{"queue_id": "", "notes": ""},
		Outputs: []workflow.HandleDefinition{
			{ID: queueTimeoutHandle, Label: "Ninguém atendeu", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "transferred", Description: "Verdadeiro quando um atendente assumiu a ligação"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "queue_id", Label: "Fila", Type: "select", OptionsSource: "call_queues", Required: true, Description: "Fila de atendimento que recebe a ligação."},
			{Key: "notes", Label: "Nota para o atendente", Type: "textarea", Placeholder: "Ex: Cliente escolheu 2ª via no menu", Description: "Aparece para o atendente antes de ele atender. Aceita variáveis."},
		},
	}
}

func (e *transferToQueueExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	transfers, err := workflow.VoiceTransfersFrom(ctx)
	if err != nil {
		return voiceNodeError(err)
	}
	queueID, _ := ctx.Node.Config["queue_id"].(string)
	queueID = strings.TrimSpace(queueID)
	if queueID == "" {
		return voiceNodeError(errQueueRequired)
	}
	notes, _ := ctx.Node.Config["notes"].(string)
	connected, err := transfers.TransferToQueue(context.Background(), workflow.QueueTransfer{
		QueueID: queueID,
		Notes:   strings.TrimSpace(workflow.Interpolate(notes, ctx.State, nil)),
		From:    ctx.Workflow.Name,
	})
	if err != nil {
		return voiceNodeError(err)
	}
	output := map[string]interface{}{"transferred": connected}
	if connected {
		return &workflow.NodeResult{Output: output, Complete: true}, nil
	}
	next := resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), queueTimeoutHandle)
	if next == "" {
		return &workflow.NodeResult{Output: output, Complete: true}, nil
	}
	return &workflow.NodeResult{NextNodeID: next, Output: output}, nil
}
