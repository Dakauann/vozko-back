package attendancetools

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/attendance"
	"vozko/domain/choicestats"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
)

const (
	choiceSubjectWorkflow   = "workflow"
	choiceSubjectCampaign   = "whatsapp_campaign"
	choiceSubjectUnofficial = "unofficial_campaign"
	choiceSubjectTemplate   = "template"
)

var (
	errUnknownChoiceSubject = errors.New("subject inválido: use workflow, whatsapp_campaign, unofficial_campaign ou template")
	errChoiceIDRequired     = errors.New("id obrigatório: use o id do fluxo, da campanha ou do template, nunca invente")
	errChoiceScopeDenied    = errors.New("sem acesso a esta campanha")
)

type ChoiceStatsReader interface {
	Workflow(ctx context.Context, scope workflow.Scope, workflowID, from, to string, version *int) (*choicestats.WorkflowReport, error)
	WhatsAppCampaign(ctx context.Context, access wc.ReportAccess, campaignID, from, to string) (*choicestats.PromptReport, error)
	UnofficialCampaign(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID, from, to string) (*choicestats.PromptReport, error)
	Template(ctx context.Context, workspaceID, templateID, from, to string) (*choicestats.PromptReport, error)
}

type ChoiceStatsDeps struct {
	Reader      ChoiceStatsReader
	Permissions workspace.CheckAccessUseCase
	Scopes      uw.DepartmentScopeSource
	Now         copilottools.Clock
}

type readChoiceStatsTool struct {
	deps ChoiceStatsDeps
}

func NewReadChoiceStatsTool(deps ChoiceStatsDeps) copilot.Tool {
	return &readChoiceStatsTool{deps: deps}
}

func (t *readChoiceStatsTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceWorkflows, Action: workspace.ActionRead}
}

func (t *readChoiceStatsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "read_choice_stats",
		Description: "Taxa de cliques (CTR) das escolhas enviadas: os nós \"Enviar Opções\" e de template de um fluxo, os botões de uma campanha " +
			"oficial (template), o menu de uma campanha não oficial ou os botões de um template em todos os envios. Traz envios aceitos, " +
			"envios com clique, taxa de cliques, texto livre e, por opção, exibições, cliques e ctr; taxas em pontos-base (10000 = 100%). " +
			"Só o primeiro clique de cada envio conta, no dia do envio, por até 30 dias. Testes de fluxo nunca contam. " +
			"Em template, template_url:<n> com tracked false é um link sem rastreio (nunca tem cliques). " +
			fmt.Sprintf("Para fluxo, o período padrão é o da tela ou os últimos 30 dias (até %d dias); campanha e template sem datas cobrem a vida inteira.", choicestats.MaxPeriodDays),
		Parameters: map[string]tools.Parameter{
			"subject":   {Type: "string", Description: "workflow, whatsapp_campaign, unofficial_campaign ou template"},
			"id":        {Type: "string", Description: "id do fluxo, da campanha ou do template (de list_workflows, campaign_dispatch ou list_templates)"},
			"date_from": {Type: "string", Description: "início YYYY-MM-DD (com date_to)"},
			"date_to":   {Type: "string", Description: "fim YYYY-MM-DD, inclusivo"},
			"version":   {Type: "number", Description: "só fluxo: uma versão publicada; omita para somar todas"},
		},
	}
}

func (t *readChoiceStatsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	subject := copilottools.TrimLower(copilottools.ArgString(args, "subject"))
	id := copilottools.ArgString(args, "id")
	if id == "" {
		return choiceFailure(errChoiceIDRequired)
	}
	from, to := copilottools.ArgString(args, "date_from"), copilottools.ArgString(args, "date_to")
	report, err := t.read(ctx, cc, subject, id, from, to, args)
	if err != nil {
		return choiceFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"subject": subject, "id": id, "report": report}}
}

func (t *readChoiceStatsTool) read(ctx context.Context, cc copilot.Context, subject, id, from, to string, args map[string]interface{}) (interface{}, error) {
	switch subject {
	case choiceSubjectWorkflow:
		window, err := attendance.ParseWindow(onScreen(from, cc.View.DateFrom), onScreen(to, cc.View.DateTo), t.deps.Now())
		if err != nil {
			return nil, choicestats.ErrInvalidPeriod
		}
		var version *int
		if v := copilottools.ArgInt(args, "version"); v != 0 {
			version = &v
		}
		scope := workflow.Scope{WorkspaceID: cc.WorkspaceID, Departments: cc.Departments, Requester: copilottools.RequesterOf(cc)}
		return t.deps.Reader.Workflow(ctx, scope, id, window.From.Format(attendance.DayLayout), window.To.Format(attendance.DayLayout), version)
	case choiceSubjectCampaign:
		if err := t.permit(cc, workspace.ResourceWhatsAppCampaigns); err != nil {
			return nil, err
		}
		access := wc.ReportAccess{WorkspaceID: cc.WorkspaceID, CampaignID: id, AllowDepartment: cc.Departments.Allows}
		return t.deps.Reader.WhatsAppCampaign(ctx, access, id, from, to)
	case choiceSubjectUnofficial:
		if err := t.permit(cc, workspace.ResourceUnofficialWhatsAppCampaigns); err != nil {
			return nil, err
		}
		scope, ok := uw.ResolveScope(t.deps.Scopes, cc.UserID, cc.WorkspaceID, cc.SystemAdmin)
		if !ok {
			return nil, errChoiceScopeDenied
		}
		return t.deps.Reader.UnofficialCampaign(ctx, cc.WorkspaceID, scope, id, from, to)
	case choiceSubjectTemplate:
		if err := t.permit(cc, workspace.ResourceWhatsAppTemplates); err != nil {
			return nil, err
		}
		return t.deps.Reader.Template(ctx, cc.WorkspaceID, id, from, to)
	}
	return nil, errUnknownChoiceSubject
}

func (t *readChoiceStatsTool) permit(cc copilot.Context, resource workspace.Resource) error {
	if cc.SystemAdmin {
		return nil
	}
	if t.deps.Permissions == nil {
		return errChoiceScopeDenied
	}
	if err := t.deps.Permissions.Execute(cc.UserID, cc.WorkspaceID, resource, workspace.ActionRead); err != nil {
		return fmt.Errorf("%w: %s:%s", errChoiceScopeDenied, resource, workspace.ActionRead)
	}
	return nil
}

func choiceFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, errChoiceScopeDenied), errors.Is(err, workflow.ErrWorkflowAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "sem acesso a este fluxo, campanha ou template"}
	case errors.Is(err, errUnknownChoiceSubject), errors.Is(err, errChoiceIDRequired):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, choicestats.ErrInvalidPeriod):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("período inválido: use date_from e date_to em YYYY-MM-DD, até %d dias", choicestats.MaxPeriodDays)}
	case errors.Is(err, choicestats.ErrInvalidVersion):
		return copilot.Result{Status: copilot.StatusError, Message: "versão inválida: use o número de uma versão publicada"}
	case errors.Is(err, workflow.ErrWorkflowNotFound), errors.Is(err, wc.ErrCampaignNotFound), errors.Is(err, uwc.ErrCampaignNotFound), errors.Is(err, template.ErrTemplateNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "não encontrado neste workspace: confira o id com list_workflows, campaign_dispatch ou list_templates"}
	case errors.Is(err, choicestats.ErrUnavailable):
		return copilot.Result{Status: copilot.StatusError, Message: "os cliques não puderam ser lidos agora; diga isso ao usuário, nunca responda zero"}
	}
	return analyticsFailure("read_choice_stats", err)
}
