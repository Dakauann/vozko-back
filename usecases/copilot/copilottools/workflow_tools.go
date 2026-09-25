package copilottools

import (
	"context"
	"errors"
	"log"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type workflowStatusArgs struct {
	WorkflowID string `json:"workflow_id" req:"true" desc:"workflow_id exato de list_workflows" id:"true"`
}

type workflowStatusTool struct {
	workflows workflow.ScopedWorkflowsUseCase
	activate  bool
}

func NewPauseWorkflowTool(workflows workflow.ScopedWorkflowsUseCase) copilot.Tool {
	return &workflowStatusTool{workflows: workflows}
}

func NewActivateWorkflowTool(workflows workflow.ScopedWorkflowsUseCase) copilot.Tool {
	return &workflowStatusTool{workflows: workflows, activate: true}
}

func (t *workflowStatusTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceWorkflows, Action: workspace.ActionUpdate}
}

func (t *workflowStatusTool) Definition() tools.Definition {
	if t.activate {
		return definition("activate_workflow",
			"Ativa uma automação pausada ou em rascunho; ela volta a rodar nas conversas. Só depois da aprovação do usuário.",
			workflowStatusArgs{})
	}
	return definition("pause_workflow",
		"Pausa uma automação ativa; ela para de rodar em novas conversas. Só depois da aprovação do usuário.",
		workflowStatusArgs{})
}

func scopeOf(cc copilot.Context) workflow.Scope {
	return workflow.Scope{WorkspaceID: cc.WorkspaceID, Departments: cc.Departments}
}

func (t *workflowStatusTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a workflowStatusArgs
	bindArgs(args, &a)
	name := "automação desconhecida ou sem acesso"
	if id, err := knownID(a.WorkflowID, "workflow_id", "list_workflows"); err == nil {
		if wf, err := t.workflows.Get(scopeOf(cc), id); err == nil {
			name = wf.Name
		}
	}
	return []copilot.Field{{Key: "workflow", Value: name}}
}

func (t *workflowStatusTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a workflowStatusArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := knownID(a.WorkflowID, "workflow_id", "list_workflows")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	change := t.workflows.Pause
	if t.activate {
		change = t.workflows.Activate
	}
	wf, err := change(scopeOf(cc), id)
	if err != nil {
		return workflowFailure(t.Definition().Name, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"workflow": wf.Name, "status": wf.Status}}
}

func workflowFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, workflow.ErrWorkflowAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta automação"}
	case errors.Is(err, workflow.ErrWorkflowNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "automação desconhecida; use os ids de list_workflows"}
	case errors.Is(err, workflow.ErrCannotActivate), errors.Is(err, workflow.ErrWorkflowNotActive), errors.Is(err, workflow.ErrInvalidStatus):
		return copilot.Result{Status: copilot.StatusError, Message: "a automação não está num estado que permita essa mudança"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível mudar a automação; confira o fluxo no editor de automações"}
}

func (t *workflowStatusTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[workflowStatusArgs](nil, cc, args)
	return err
}
