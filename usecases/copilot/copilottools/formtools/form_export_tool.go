package formtools

import (
	"context"
	"encoding/json"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/report"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	report_usecase "vozko/usecases/report"
	report_renderers "vozko/usecases/report/renderers"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type exportFormResponsesArgs struct {
	formRef
	periodArgs
	Format string `json:"format" enum:"csv,xlsx" desc:"formato da planilha; padrão csv"`
}

func (a exportFormResponsesArgs) format() report.Format {
	if strings.TrimSpace(a.Format) == "" {
		return report.FormatCSV
	}
	return report.Format(strings.TrimSpace(a.Format))
}

type exportWhatsAppFormResponsesTool struct {
	gate
	deps FormDeps
}

func NewExportWhatsAppFormResponsesTool(deps FormDeps) copilot.Tool {
	return &exportWhatsAppFormResponsesTool{gate: gated(true, workspace.ResourceWhatsAppFormResponses, workspace.ActionExport, wf.CapabilityExport), deps: deps}
}

func (t *exportWhatsAppFormResponsesTool) Definition() tools.Definition {
	return copilottools.Definition("export_whatsapp_form_responses",
		"Exporta as respostas de um formulário do WhatsApp para uma planilha (csv ou xlsx), só das conversas que o usuário alcança, opcionalmente num período. "+
			"O arquivo é gerado em segundo plano e fica em Relatórios; só quem pediu pode baixar. Só depois da aprovação do usuário.",
		exportFormResponsesArgs{})
}

func (t *exportWhatsAppFormResponsesTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[exportFormResponsesArgs](nil, cc, args)
	if err != nil {
		return err
	}
	period, err := a.period()
	if err != nil {
		return err
	}
	actor := actorOf(cc)
	detail, err := a.resolve(ctx, t.deps.Forms, actor)
	if err != nil {
		return refusalOf(err)
	}
	if _, err := t.deps.Forms.ListResponses(ctx, actor, wf.ResponseQuery{FormID: detail.Form.ID, From: period.From, To: period.To, Limit: 1}); err != nil {
		return refusalOf(err)
	}
	return nil
}

func (t *exportWhatsAppFormResponsesTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a exportFormResponsesArgs
	copilottools.BindArgs(args, &a)
	form := "formulário desconhecido ou sem acesso"
	if detail, err := a.resolve(ctx, t.deps.Forms, actorOf(cc)); err == nil {
		form = detail.Form.Name
	}
	return []copilot.Field{
		{Key: "form", Value: form},
		{Key: "period", Value: a.shown()},
		{Key: "format", Value: strings.ToUpper(string(a.format()))},
	}
}

func (t *exportWhatsAppFormResponsesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in exportFormResponsesArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Reports == nil {
		return failure(report_usecase.ErrNotConfigured)
	}
	detail, err := in.resolve(ctx, t.deps.Forms, actorOf(cc))
	if err != nil {
		return failure(err)
	}
	params, err := exportParams(detail, in)
	if err != nil {
		return failure(err)
	}
	job, err := t.deps.Reports.Create(report_usecase.CreateInput{
		WorkspaceID: cc.WorkspaceID, RequestedBy: cc.UserID, RequestedByAdmin: cc.SystemAdmin,
		Kind: report.KindWhatsAppFormResponses, Format: in.format(), Locale: cc.Locale, Params: params, FromClient: true,
	})
	if err != nil {
		return failure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"report_id": job.ID, "status": string(job.Status), "format": string(job.Format), "form": detail.Form.Name,
		"next": copilottools.ReportQueuedNext,
	}}
}

func exportParams(detail whatsappform_usecase.FormDetail, in exportFormResponsesArgs) (json.RawMessage, error) {
	return json.Marshal(report_renderers.WhatsAppFormResponsesParams{
		FormID: detail.Form.ID, From: strings.TrimSpace(in.From), To: strings.TrimSpace(in.To),
	})
}
