package node_executors

import (
	"time"
	"vozko/domain/workflow"
)

type waitDurationExecutor struct{}

func NewWaitDurationExecutor() workflow.NodeExecutor {
	return &waitDurationExecutor{}
}

func (e *waitDurationExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeWaitDuration,
		Category:    workflow.NodeCategoryWait,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Aguardar Duração",
		Description: "Pausa o fluxo por um tempo determinado.",
		Icon:        "Timer",
		Guidance: workflow.NodeGuidance{
			When: "Para pausar o fluxo por um tempo fixo.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "completed", Label: "Tempo passou"},
			{ID: "message_received", Label: "Mensagem recebida", Optional: true},
		},
		DefaultConfig: map[string]interface{}{
			"seconds": float64(60),
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "seconds", Label: "Duração (segundos)", Type: "number", Placeholder: "60", Required: true},
		},
	}
}

func (e *waitDurationExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	seconds, _ := ctx.Node.Config["seconds"].(float64)
	if seconds <= 0 {

		if minutes, ok := ctx.Node.Config["minutes"].(float64); ok && minutes > 0 {
			seconds = minutes * 60
		}
	}
	if seconds <= 0 {
		return nil, workflow.ErrNodeConfigMissing
	}
	wakeAt := time.Now().UTC().Add(time.Duration(seconds * float64(time.Second))).UnixMilli()
	return &workflow.NodeResult{
		Wait: &workflow.WaitInstruction{
			WakeAt: wakeAt,
			Reason: workflow.WaitReasonDuration,
		},
	}, nil
}
