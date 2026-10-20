package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/usecases/agentctx"
	workflow_usecase "vozko/usecases/workflow"
)

const (
	RunWorkflowToolName         = workflow.AgentRunWorkflowTool
	AgentWorkflowsOptionsSource = "agent_workflows"
	runWorkflowConfigKey        = "workflow_id"
	runWorkflowInputsParam      = "inputs"
)

const (
	runWorkflowStarted     = "Fluxo iniciado. Não afirme conclusão; o resultado chega depois."
	runWorkflowInactive    = "Fluxo inativo. Não prometa; diga que vai verificar."
	runWorkflowNoChat      = "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento."
	runWorkflowNotFound    = "Fluxo não encontrado. Não prometa; diga que vai verificar."
	runWorkflowNotCallable = "Este fluxo não aceita chamadas de agente. Não prometa; diga que vai verificar."
	runWorkflowUnreadable  = "Não foi possível consultar o fluxo agora. Não prometa; diga que vai verificar."
	runWorkflowBusy        = "Há fluxos demais em execução neste momento. Diga ao cliente que vai verificar e tente de novo mais tarde."
	runWorkflowUnavailable = "Este fluxo está indisponível. Não chame esta ferramenta."
)

type WorkflowFinder = workflow.PublishedReader

type AgentWorkflowStarter interface {
	StartForConversation(in workflow_usecase.ConversationStart) (workflow_usecase.StartedRun, error)
}

type runWorkflowTool struct {
	workflows WorkflowFinder
	starter   AgentWorkflowStarter
	steps     workflow_usecase.StepNamer
	now       func() time.Time
}

func NewRunWorkflowToolUseCase(workflows WorkflowFinder, starter AgentWorkflowStarter, steps workflow_usecase.StepNamer) tools.Handler {
	if workflows == nil || starter == nil || steps == nil {
		return nil
	}
	return &runWorkflowTool{workflows: workflows, starter: starter, steps: steps, now: func() time.Time { return time.Now().UTC() }}
}

func (t *runWorkflowTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               RunWorkflowToolName,
		DisplayName:        "Executar fluxo",
		DisplayDescription: "Executa um fluxo de automação na conversa atual, com os campos que o gatilho Chamado por agente IA do fluxo pede. Cada fluxo escolhido vira uma ferramenta própria do agente.",
		Description: `Executa um fluxo de automação na conversa atual.

O fluxo roda em segundo plano: o resultado não volta nesta chamada. Depois de chamar, não afirme que terminou; diga ao cliente que está processando.`,
		Parameters: map[string]tools.Parameter{
			runWorkflowInputsParam: {
				Type:               "object",
				Description:        "Campos pedidos pelo fluxo.",
				DisplayName:        "Campos",
				DisplayDescription: "Campos enviados ao gatilho do fluxo",
			},
		},
		ConfigSchema: map[string]tools.ConfigParameter{
			runWorkflowConfigKey: {
				Type:               "string",
				Description:        "Fluxo de mensagens ativo com o gatilho Chamado por agente IA.",
				DisplayName:        "Fluxo",
				DisplayDescription: "Fluxo ativo com o gatilho Chamado por agente IA.",
				OptionsSource:      AgentWorkflowsOptionsSource,
				Required:           true,
			},
		},
		RequiredConfig: []string{runWorkflowConfigKey},
		RequiresConfig: true,
		BindingKey:     runWorkflowConfigKey,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryAgentAction,
	}
}

func (t *runWorkflowTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	def := t.Definition()
	w, inputs, ok := t.callableWorkflow(ctx.WorkspaceID, configString(ctx.Config, runWorkflowConfigKey))
	if !ok {
		def.Description = runWorkflowUnavailable
		return def
	}
	def.Description = fmt.Sprintf("Executa o fluxo %q na conversa atual.", w.Name)
	if w.Description != "" {
		def.Description += "\n\n" + w.Description
	}
	def.Description += "\n\nO fluxo roda em segundo plano: o resultado não volta nesta chamada. Depois de chamar, não afirme que terminou; diga ao cliente que está processando."
	raw, _ := w.Graph.TriggerNodeByType(workflow.TriggerAgentCall).Config[workflow.AgentCallInputsKey].([]interface{})
	if param, section, hasInputs := tools.SchemaObjectParameter("Campos pedidos pelo fluxo", "Campos do fluxo", raw); hasInputs {
		def.Parameters = map[string]tools.Parameter{runWorkflowInputsParam: param}
		def.Description += section
	}
	for _, in := range inputs {
		if in.Required {
			def.Required = []string{runWorkflowInputsParam}
			break
		}
	}
	return def
}

func (t *runWorkflowTool) callableWorkflow(workspaceID, workflowID string) (*workflow.Workflow, []workflow.AgentCallInput, bool) {
	if workspaceID == "" || workflowID == "" {
		return nil, nil, false
	}
	w, err := workflow.LoadPublished(t.workflows, workflowID)
	if err != nil {
		return nil, nil, false
	}
	inputs, err := w.CallableByAgentIn(workspaceID)
	if err != nil {
		return nil, nil, false
	}
	return w, inputs, true
}

func (t *runWorkflowTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *runWorkflowTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID := configString(config, "__workspace_id")
	entryID := configString(config, "__entry_id")
	entryType := configString(config, "__entry_type")
	agentID := agentIDOf(ctx, config)
	workflowID := configString(config, runWorkflowConfigKey)
	if workspaceID == "" || entryID == "" || entryType == "" || agentID == "" {
		return refusal(runWorkflowNoChat), nil
	}
	attrs := []any{slog.String("workspace_id", workspaceID), slog.String("workflow_id", workflowID), slog.String("entry_id", entryID), slog.String("agent_id", agentID)}

	w, err := workflow.LoadPublished(t.workflows, workflowID)
	if err != nil && !errors.Is(err, workflow.ErrWorkflowNotPublished) {
		slog.Error("tools.run_workflow.workflow_unreadable", append(attrs, slog.String("error", err.Error()))...)
		return refusal(runWorkflowUnreadable), nil
	}
	if err != nil {
		return startRefusal(err), nil
	}
	declared, err := w.CallableByAgentIn(workspaceID)
	if err != nil {
		return startRefusal(err), nil
	}
	values, refused := t.inputs(declared, params)
	if refused != "" {
		return refusal(refused), nil
	}

	started, err := t.starter.StartForConversation(workflow_usecase.ConversationStart{
		WorkspaceID: workspaceID,
		WorkflowID:  workflowID,
		EntryID:     entryID,
		EntryType:   entryType,
		TriggerType: workflow.TriggerAgentCall,
		StartedBy:   workflow.StartedByAgent(agentID),
		Data:        workflow.AgentCallRunData(values, agentID, agentNameOf(ctx, agentID)),
	})
	if err != nil {
		slog.Warn("tools.run_workflow.start_refused", append(attrs, slog.String("error", err.Error()))...)
		return startRefusal(err), nil
	}
	if started.AlreadyRunning {
		return tools.ExecutionResult{RunID: started.RunID, Result: fmt.Sprintf(
			"Este fluxo já está em execução nesta conversa há %s, na etapa %s. Não inicie de novo e não afirme conclusão; o resultado chega depois.",
			workflow.ElapsedPT(t.now().Sub(started.StartedAt)), t.steps.StepName(started.CurrentNodeType),
		)}, nil
	}
	slog.Info("tools.run_workflow.started", append(attrs, slog.String("run_id", started.RunID))...)
	return tools.ExecutionResult{RunID: started.RunID, Result: runWorkflowStarted}, nil
}

func (t *runWorkflowTool) inputs(declared []workflow.AgentCallInput, params map[string]interface{}) (map[string]interface{}, string) {
	raw := params[runWorkflowInputsParam]
	values, isObject := raw.(map[string]interface{})
	if raw != nil && !isObject {
		return nil, "O parâmetro inputs deve ser um objeto com os campos do fluxo."
	}
	cleaned, missing, err := workflow.ValidateAgentCallInputs(declared, values)
	if err != nil {
		return nil, fmt.Sprintf("Campo com tipo errado: %v. Corrija e chame de novo.", err)
	}
	if len(missing) > 0 {
		return nil, fmt.Sprintf("Faltam campos obrigatórios: %s. Peça ao cliente antes de chamar de novo.", strings.Join(missing, ", "))
	}
	return cleaned, ""
}

func startRefusal(err error) tools.ExecutionResult {
	switch {
	case errors.Is(err, workflow.ErrWorkflowNotActive):
		return inactiveRefusal()
	case errors.Is(err, workflow.ErrWorkflowNotFound), errors.Is(err, workflow.ErrInvalidWorkflowType):
		return refusal(runWorkflowNotFound)
	case errors.Is(err, workflow.ErrTriggerNotInWorkflow), errors.Is(err, workflow.ErrAgentCallInputsInvalid):
		return refusal(runWorkflowNotCallable)
	case errors.Is(err, workflow.ErrWorkspaceAtCapacity):
		return refusal(runWorkflowBusy)
	}
	return refusal(runWorkflowUnreadable)
}

func inactiveRefusal() tools.ExecutionResult {
	result := refusal(runWorkflowInactive)
	result.ErrorCode = workflow.WorkflowNotActiveCode
	return result
}

func agentNameOf(ctx context.Context, agentID string) string {
	if current, ok := agentctx.AgentFromContext(ctx); ok && current != nil && current.ID == agentID {
		return current.Name
	}
	return ""
}

func refusal(text string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: text, IsError: true}
}

var _ tools.ContextualHandler = (*runWorkflowTool)(nil)
