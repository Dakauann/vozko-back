package workflow_usecase

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

var (
	errReproduceForbidden    = errors.New("Reproduzir uma execução exige permissão de leitura de fluxos.")
	errReproduceRunMissing   = errors.New("Execução não encontrada, ou a conversa dela não está visível para você.")
	errReproduceRunUnread    = errors.New("Não foi possível ler a execução agora. Tente de novo.")
	errReproduceOtherFlow    = errors.New("Esta execução é de outro fluxo.")
	errReproduceNoTrigger    = errors.New("Esta execução não guardou os dados do gatilho (começou antes desse registro e a etapa do gatilho não tem saída), então não dá para reproduzi-la.")
	errReproduceVoice        = errors.New("Reproduzir execuções só está disponível em fluxos de mensagens.")
	errReproduceRunIDMissing = errors.New("Informe o runId da execução a reproduzir.")
)

type simReadyPayload struct {
	WorkflowID           string                      `json:"workflowId"`
	TriggerType          string                      `json:"triggerType"`
	RealCallNodes        []string                    `json:"realCallNodes"`
	Triggers             []workflow.TriggerInputSpec `json:"triggers"`
	DefaultTriggerNodeID string                      `json:"defaultTriggerNodeId"`
}

type simTriggerRef struct {
	NodeID      string `json:"nodeId"`
	TriggerType string `json:"triggerType"`
}

func simTriggerOf(node *workflow.Node) simTriggerRef {
	return simTriggerRef{NodeID: node.ID, TriggerType: string(node.Type)}
}

type simStartedPayload struct {
	RunID         string           `json:"runId"`
	WorkflowID    string           `json:"workflowId"`
	RealCallNodes []string         `json:"realCallNodes"`
	Reproducing   *simReproduction `json:"reproducing,omitempty"`
	Trigger       simTriggerRef    `json:"trigger"`
}

type simReproduction struct {
	RunID           string   `json:"runId"`
	WorkflowVersion *int     `json:"workflowVersion,omitempty"`
	PinnedNodes     []string `json:"pinnedNodes"`

	seed          map[string]interface{}
	pins          map[string]workflow.NodePin
	triggerNodeID string
}

func (s *wsWorkflowSimulation) prepareReproduction(session SimulationSession, wf *workflow.Workflow, runID string, behaviors workflow.TestBehaviors) (*simReproduction, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, errReproduceRunIDMissing
	}
	if wf.Type == workflow.WorkflowTypeVoice {
		return nil, errReproduceVoice
	}
	if s.deps.Runs == nil || s.deps.Permissions == nil ||
		!s.deps.Permissions.HasWorkspacePermission(session.Viewer.UserID, session.WorkspaceID, string(workspace.ResourceWorkflows), string(workspace.ActionRead), session.Viewer.SystemAdmin) {
		return nil, errReproduceForbidden
	}
	detail, err := s.deps.Runs.PickSteps(session.Viewer, session.WorkspaceID, runID, workflow.ReproductionPick(&wf.Graph, behaviors))
	if errors.Is(err, workflow.ErrRunNotFound) || (err == nil && (detail == nil || detail.Run == nil)) {
		return nil, errReproduceRunMissing
	}
	if err != nil {
		slog.Warn("workflow.simulation.reproduce_read_failed",
			slog.String("run_id", runID), slog.String("workspace_id", session.WorkspaceID), slog.String("error", err.Error()))
		return nil, errReproduceRunUnread
	}
	if detail.Run.WorkflowID != wf.ID {
		return nil, errReproduceOtherFlow
	}
	seed, err := workflow.ReproductionSeed(detail.Run, detail.Logs)
	if err != nil {
		return nil, errReproduceNoTrigger
	}
	return &simReproduction{
		RunID:           detail.Run.ID,
		WorkflowVersion: detail.Run.WorkflowVersion,
		seed:            seed,
		pins:            workflow.ReproductionPins(&wf.Graph, detail.Logs, detail.Run.ID, time.Now().UTC(), behaviors),
		triggerNodeID:   detail.Run.TriggerNodeID,
	}, nil
}
