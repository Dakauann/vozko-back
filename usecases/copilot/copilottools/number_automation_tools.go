package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/stage"
	"vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type NumberAutomation interface {
	Get(workspaceID, phoneID string) (wc.ReceptiveSettings, error)
	Update(workspaceID, phoneID string, settings wc.ReceptiveSettings) (wc.ReceptiveSettings, error)
}

type NumberAutomationDeps struct {
	Numbers   NumberAutomation
	Agents    agent.GetAgentUseCase
	Workflows workflow.ScopedWorkflowsUseCase
	Funnels   stage.ListFunnelStagesUseCase
}

const unknownFunnel = "funil desconhecido"

var errNotNumberOwner = fmt.Errorf("%w: só o workspace dono do número configura quem atende; um workspace com acesso concedido usa o número em campanhas, mas não no atendimento receptivo", errInvalidArgs)

func (d NumberAutomationDeps) current(cc copilot.Context, phoneID string) (wc.ReceptiveSettings, error) {
	settings, err := d.Numbers.Get(cc.WorkspaceID, phoneID)
	return settings, numberAutomationError(err)
}

func numberAutomationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, wc.ErrReceptiveNotOwner):
		return errNotNumberOwner
	case errors.Is(err, wc.ErrCampaignBusinessPhoneNotFound):
		return fmt.Errorf("%w: número desconhecido; use o business_phone_id exato de list_business_phones", errInvalidArgs)
	default:
		return err
	}
}

func (d NumberAutomationDeps) agentName(cc copilot.Context, id string) string {
	if a, err := ownedAgent(d.Agents, cc, id); err == nil {
		return a.Name
	}
	return "agente desconhecido"
}

func (d NumberAutomationDeps) workflowName(cc copilot.Context, id string) string {
	if wf, err := d.Workflows.Get(scopeOf(cc), strings.TrimSpace(id)); err == nil && wf != nil {
		return wf.Name
	}
	return "automação desconhecida ou sem acesso"
}

func (d NumberAutomationDeps) funnelName(cc copilot.Context, id string) string {
	funnels, err := d.Funnels.Execute(cc.WorkspaceID)
	if err != nil {
		return unknownFunnel
	}
	for _, f := range funnels {
		if f.PipelineID == id {
			return f.PipelineName
		}
	}
	return unknownFunnel
}

type numberAutomationArgs struct {
	BusinessPhoneID string `json:"business_phone_id" req:"true" id:"true" desc:"business_phone_id exato de list_business_phones"`
}

type numberAutomationTool struct{ deps NumberAutomationDeps }

func NewNumberAutomationTool(deps NumberAutomationDeps) copilot.Tool {
	return &numberAutomationTool{deps: deps}
}

func (t *numberAutomationTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceBusinessPhones, Action: workspace.ActionRead}
}

func (t *numberAutomationTool) Definition() tools.Definition {
	return definition("number_automation",
		"Mostra quem atende as conversas receptivas de um número do WhatsApp oficial (agente, automação ou só a equipe), o funil e as análises. "+
			"Conversas de campanhas seguem a própria campanha. Só o workspace dono do número vê e configura.",
		numberAutomationArgs{})
}

func (t *numberAutomationTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a numberAutomationArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	settings, err := t.deps.current(cc, a.BusinessPhoneID)
	if err != nil {
		return numberAutomationFailure("number_automation", err)
	}
	data := map[string]interface{}{
		"answered_by":  string(settings.Answerer()),
		"analysis":     settings.EnableAnalysis,
		"auto_staging": settings.EnableAutoStaging,
		"auto_memory":  settings.EnableAutoMemory,
	}
	switch settings.Answerer() {
	case wc.AnsweredByAgent:
		data["agent_id"], data["agent"] = settings.AgentID, t.deps.agentName(cc, settings.AgentID)
	case wc.AnsweredByWorkflow:
		data["workflow_id"], data["workflow"] = settings.WorkflowID, t.deps.workflowName(cc, settings.WorkflowID)
	}
	if settings.PipelineID != "" {
		data["pipeline_id"], data["pipeline"] = settings.PipelineID, t.deps.funnelName(cc, settings.PipelineID)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

type configureNumberAutomationArgs struct {
	BusinessPhoneID string `json:"business_phone_id" req:"true" id:"true" desc:"business_phone_id exato de list_business_phones"`
	AnsweredBy      string `json:"answered_by" req:"true" enum:"agent,workflow,none" desc:"agent (um agente responde), workflow (uma automação conduz) ou none (só a equipe responde)"`
	AgentID         string `json:"agent_id" id:"true" desc:"agent_id exato de list_agents, quando answered_by é agent"`
	WorkflowID      string `json:"workflow_id" id:"true" desc:"workflow_id exato de list_workflows, quando answered_by é workflow"`
	PipelineID      string `json:"pipeline_id" id:"true" desc:"funil das conversas do número; omita para manter o atual"`
	Analysis        *bool  `json:"analysis" desc:"analisar as conversas; omita para manter"`
	AutoStaging     *bool  `json:"auto_staging" desc:"mover as conversas de etapa no funil sozinho; omita para manter"`
	AutoMemory      *bool  `json:"auto_memory" desc:"guardar o que o cliente conta na memória do contato; omita para manter"`
}

func (a configureNumberAutomationArgs) answererID() string {
	if wc.Answerer(a.AnsweredBy) == wc.AnsweredByWorkflow {
		return strings.TrimSpace(a.WorkflowID)
	}
	return strings.TrimSpace(a.AgentID)
}

func (a configureNumberAutomationArgs) apply(current wc.ReceptiveSettings) wc.ReceptiveSettings {
	next := current.AnsweredBy(wc.Answerer(a.AnsweredBy), a.answererID())
	if pipeline := strings.TrimSpace(a.PipelineID); pipeline != "" {
		next.PipelineID = pipeline
	}
	for _, toggle := range []struct {
		value *bool
		field *bool
	}{{a.Analysis, &next.EnableAnalysis}, {a.AutoStaging, &next.EnableAutoStaging}, {a.AutoMemory, &next.EnableAutoMemory}} {
		if toggle.value != nil {
			*toggle.field = *toggle.value
		}
	}
	return next
}

type configureNumberAutomationTool struct{ deps NumberAutomationDeps }

func NewConfigureNumberAutomationTool(deps NumberAutomationDeps) copilot.Tool {
	return &configureNumberAutomationTool{deps: deps}
}

func (t *configureNumberAutomationTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceBusinessPhones, Action: workspace.ActionUpdate}
}

func (t *configureNumberAutomationTool) Definition() tools.Definition {
	return definition("configure_number_automation",
		"Define quem atende as conversas receptivas de um número do WhatsApp oficial (as que o cliente começa ou que vêm de anúncios): um agente, uma automação ou só a equipe; "+
			"e também o funil e as análises. Vale para todas as conversas receptivas do número, como nos outros canais. "+
			"Só o workspace dono do número configura. Só depois da aprovação do usuário.",
		configureNumberAutomationArgs{})
}

func (t *configureNumberAutomationTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	var a configureNumberAutomationArgs
	if err := decodeArgs(args, &a); err != nil {
		return err
	}
	switch wc.Answerer(a.AnsweredBy) {
	case wc.AnsweredByAgent:
		if _, err := ownedAgent(t.deps.Agents, cc, a.AgentID); err != nil {
			return err
		}
	case wc.AnsweredByWorkflow:
		if wf, err := t.deps.Workflows.Get(scopeOf(cc), a.answererID()); err != nil || wf == nil {
			return fmt.Errorf("%w: automação não encontrada neste workspace; use o id exato de list_workflows", errInvalidArgs)
		}
	}
	if pipeline := strings.TrimSpace(a.PipelineID); pipeline != "" && t.deps.funnelName(cc, pipeline) == unknownFunnel {
		return fmt.Errorf("%w: funil não encontrado neste workspace", errInvalidArgs)
	}
	_, err := t.deps.current(cc, a.BusinessPhoneID)
	return err
}

func (t *configureNumberAutomationTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a configureNumberAutomationArgs
	bindArgs(args, &a)
	var fields []copilot.Field
	switch wc.Answerer(a.AnsweredBy) {
	case wc.AnsweredByAgent:
		fields = append(fields, copilot.Field{Key: "agent", Value: t.deps.agentName(cc, a.AgentID)})
	case wc.AnsweredByWorkflow:
		fields = append(fields, copilot.Field{Key: "workflow", Value: t.deps.workflowName(cc, a.WorkflowID)})
	default:
		fields = append(fields, copilot.Field{Key: "answeredBy", Value: "Só a equipe"})
	}
	if pipeline := strings.TrimSpace(a.PipelineID); pipeline != "" {
		fields = append(fields, copilot.Field{Key: "pipeline", Value: t.deps.funnelName(cc, pipeline)})
	}
	for _, toggle := range []struct {
		key   string
		value *bool
	}{{"analysis", a.Analysis}, {"autoStaging", a.AutoStaging}, {"autoMemory", a.AutoMemory}} {
		if toggle.value != nil {
			fields = append(fields, copilot.Field{Key: toggle.key, Value: fmt.Sprint(*toggle.value)})
		}
	}
	return fields
}

func (t *configureNumberAutomationTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a configureNumberAutomationArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	current, err := t.deps.current(cc, a.BusinessPhoneID)
	if err != nil {
		return numberAutomationFailure("configure_number_automation", err)
	}
	saved, err := t.deps.Numbers.Update(cc.WorkspaceID, a.BusinessPhoneID, a.apply(current))
	if err != nil {
		return numberAutomationFailure("configure_number_automation", numberAutomationError(err))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"answered_by": string(saved.Answerer())}}
}

func numberAutomationFailure(tool string, err error) copilot.Result {
	if errors.Is(err, errInvalidArgs) {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível consultar ou salvar a automação do número agora; tente de novo"}
}
