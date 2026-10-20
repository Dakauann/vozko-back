package formtools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type createFormAutomationTool struct {
	gate
	deps FormDeps
}

func NewCreateFormAutomationTool(deps FormDeps) copilot.Tool {
	return &createFormAutomationTool{gate: gated(true, workspace.ResourceWorkflows, workspace.ActionCreate, wf.CapabilityView, whatsappform_usecase.CapabilityCreateWorkflows), deps: deps}
}

func (t *createFormAutomationTool) Definition() tools.Definition {
	return copilottools.Definition("create_form_automation",
		"Cria, em rascunho, uma automação que roda quando alguém responde o formulário do WhatsApp: ela parte do gatilho de formulário respondido, "+
			"etiqueta a conversa e abre uma oportunidade no funil padrão. A automação não roda até o usuário revisar e ativar. Só depois da aprovação do usuário.",
		formRef{})
}

func (t *createFormAutomationTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	ref, err := copilottools.ValidateArgs[formRef](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := activeForm(ctx, t.deps.Forms, cc, ref); err != nil {
		return refusalOf(err)
	}
	return nil
}

func (t *createFormAutomationTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var ref formRef
	copilottools.BindArgs(args, &ref)
	detail, err := ref.resolve(ctx, t.deps.Forms, actorOf(cc))
	if err != nil {
		return []copilot.Field{{Key: "form", Value: "formulário desconhecido ou sem acesso"}}
	}
	return []copilot.Field{
		{Key: "form", Value: detail.Form.Name},
		{Key: "workflow", Value: "nova automação em rascunho, disparada pelas respostas de " + detail.Form.Name},
	}
}

func (t *createFormAutomationTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var ref formRef
	if err := copilottools.DecodeArgs(args, &ref); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a := actorOf(cc)
	detail, err := ref.resolve(ctx, t.deps.Forms, a)
	if err != nil {
		return failure(err)
	}
	workflowID, err := t.deps.Forms.ScaffoldAutomation(ctx, a, detail.Form.ID)
	if err != nil {
		return failure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"workflow_id": workflowID, "form": detail.Form.Name,
		"next": "a automação foi criada em rascunho; o usuário revisa e ativa na tela de Automações (activate_workflow ativa quando ele pedir)",
	}}
}
