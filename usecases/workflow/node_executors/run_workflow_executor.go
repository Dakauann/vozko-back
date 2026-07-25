package node_executors

import (
	"vozko/domain/workflow"
)

type SubWorkflowRunner interface {
	RunSubWorkflow(workspaceID, workflowID, entryID, entryType string, initialState workflow.RunState) (*workflow.RunState, error)
}

type runWorkflowExecutor struct {
	runner SubWorkflowRunner
}

func NewRunWorkflowExecutor(runner SubWorkflowRunner) workflow.NodeExecutor {
	return &runWorkflowExecutor{runner: runner}
}

func (e *runWorkflowExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionRunWorkflow,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Executar Sub-Fluxo",
		Description: "Executa outro fluxo de trabalho e retorna o resultado.",
		Icon:        "GitMerge",
		Guidance: workflow.NodeGuidance{
			When:     "Para executar outro workflow como sub-fluxo e usar o resultado.",
			Behavior: "As variáveis produzidas pelo sub-fluxo retornam ao fluxo atual com o prefixo 'sub_'.",
		},
		DefaultConfig: map[string]interface{}{
			"workflow_id":    "",
			"pass_variables": true,
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "status", Description: "Status da execução do sub-fluxo (completed/error)"},
			{Key: "sub_state", Description: "Estado final do sub-fluxo (mapa de variáveis)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "workflow_id", Label: "Fluxo de trabalho", Type: "select", Required: true, OptionsSource: "workflows"},
			{Key: "pass_variables", Label: "Passar variáveis atuais", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "true", Label: "Sim"},
				{Value: "false", Label: "Não"},
			}},
		},
	}
}

func (e *runWorkflowExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	workflowID, _ := ctx.Node.Config["workflow_id"].(string)
	if workflowID == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	workflowID = workflow.Interpolate(workflowID, ctx.State, nil)

	passVars := true
	if pv, ok := ctx.Node.Config["pass_variables"].(string); ok && pv == "false" {
		passVars = false
	}
	if pv, ok := ctx.Node.Config["pass_variables"].(bool); ok {
		passVars = pv
	}

	initialState := workflow.NewRunState()
	if passVars {
		for k, v := range ctx.State.Vars {
			initialState.Set(k, v)
		}
	}

	if e.runner == nil {
		return &workflow.NodeResult{
			Output: map[string]interface{}{
				"status":    "error",
				"sub_state": nil,
			},
			Error: "sub-workflow runner not configured",
		}, nil
	}

	resultState, err := e.runner.RunSubWorkflow(
		ctx.Workflow.WorkspaceID,
		workflowID,
		ctx.Run.EntryID,
		ctx.Run.EntryType,
		initialState,
	)
	if err != nil {
		return &workflow.NodeResult{
			Output: map[string]interface{}{
				"status":    "error",
				"sub_state": nil,
			},
			Error: err.Error(),
		}, nil
	}

	if resultState != nil {
		for k, v := range resultState.Vars {
			ctx.State.Set("sub_"+k, v)
		}
	}

	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"status":    "completed",
			"sub_state": resultState.Vars,
		},
	}, nil
}
