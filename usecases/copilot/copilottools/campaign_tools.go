package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	tmpl "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

const campaignSampleRows = 3

type CampaignDeps struct {
	Preview   wc.ImportPreviewUseCase
	Create    wc.CreateCampaignUseCase
	Start     wc.StartCampaignUseCase
	Access    wc.CampaignAccessUseCase
	Templates tmpl.WorkspaceTemplatesUseCase
	Phones    businessphone.WorkspacePhonesUseCase
	Costs     tmpl.TemplateCostReader
	Balance   balance.BalanceReader
}

type campaignFileArgs struct {
	MediaID         string   `json:"media_id" req:"true" desc:"media_id da planilha anexada na conversa (CSV)" id:"true"`
	TemplateID      string   `json:"template_id" req:"true" desc:"template_id exato de list_templates" id:"true"`
	NumberColumn    string   `json:"number_column" desc:"coluna com o telefone; omita para detectar (numero, telefone, phone...)"`
	NameColumn      string   `json:"name_column" desc:"coluna com o nome do contato"`
	VariableColumns []string `json:"variable_columns" desc:"uma coluna por variável do modelo, na ordem de {{1}}, {{2}}...; omita para usar var1, var2..."`
}

func (a campaignFileArgs) request(cc copilot.Context) (wc.ImportRequest, error) {
	mediaID, err := knownID(a.MediaID, "media_id", "anexos da conversa")
	if err != nil {
		return wc.ImportRequest{}, err
	}
	templateID, err := knownID(a.TemplateID, "template_id", "list_templates")
	if err != nil {
		return wc.ImportRequest{}, err
	}
	return wc.ImportRequest{
		WorkspaceID: cc.WorkspaceID,
		MediaID:     mediaID,
		TemplateID:  templateID,
		Mapping:     wc.ColumnMapping{Number: a.NumberColumn, Name: a.NameColumn, Variables: a.VariableColumns},
	}, nil
}

type previewCampaignImportTool struct{ deps CampaignDeps }

func NewPreviewCampaignImportTool(deps CampaignDeps) copilot.Tool {
	return &previewCampaignImportTool{deps: deps}
}

func (t *previewCampaignImportTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionCreate}
}

func (t *previewCampaignImportTool) Definition() tools.Definition {
	return definition("preview_campaign_import",
		"Simula a importação de uma planilha para uma campanha, sem criar nada: lê o arquivo anexado, liga as colunas às "+
			"variáveis do modelo, valida os telefones como a importação de contatos e aponta linhas inválidas, sem variável ou "+
			"repetidas (com o número da linha). Mostra também o custo estimado e o saldo. Use antes de create_campaign.",
		campaignFileArgs{})
}

func (t *previewCampaignImportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a campaignFileArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	req, err := a.request(cc)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return campaignFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: previewData(preview)}
}

func previewData(p *wc.ImportPreview) map[string]interface{} {
	sample := p.Rows
	if len(sample) > campaignSampleRows {
		sample = sample[:campaignSampleRows]
	}
	return map[string]interface{}{
		"headers":          p.Headers,
		"mapping":          p.Mapping,
		"variables":        p.Variables,
		"total_rows":       p.TotalRows,
		"valid_rows":       p.ValidRows,
		"issue_counts":     p.IssueCounts,
		"issues":           p.Issues,
		"sample":           sample,
		"unit_cost":        formatUSD(p.UnitCostMicros),
		"estimated_cost":   formatUSD(p.CostMicros),
		"balance":          formatUSD(p.BalanceMicros),
		"balance_suffices": p.Affordable,
	}
}

type createCampaignArgs struct {
	campaignFileArgs
	Name            string `json:"name" req:"true" desc:"nome da campanha"`
	BusinessPhoneID string `json:"business_phone_id" req:"true" desc:"business_phone_id exato de list_business_phones" id:"true"`
	DepartmentID    string `json:"department_id" desc:"departamento (list_departments); só quando o usuário tem mais de um" id:"true"`
}

type createCampaignTool struct{ deps CampaignDeps }

func NewCreateCampaignTool(deps CampaignDeps) copilot.Tool { return &createCampaignTool{deps: deps} }

func (t *createCampaignTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionCreate}
}

func (t *createCampaignTool) Definition() tools.Definition {
	return definition("create_campaign",
		"Cria uma campanha do WhatsApp oficial com as linhas válidas da planilha anexada (as inválidas ficam de fora, como em "+
			"preview_campaign_import). A campanha fica criada e parada: para enviar use start_campaign. Só depois da aprovação do usuário.",
		createCampaignArgs{})
}

func (t *createCampaignTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createCampaignArgs
	bindArgs(args, &a)
	fields := []copilot.Field{
		{Key: "name", Value: strings.TrimSpace(a.Name)},
		{Key: "phone", Value: phoneLabel(t.deps.Phones, cc, a.BusinessPhoneID)},
	}
	req, err := a.request(cc)
	if err != nil {
		return append(fields, copilot.Field{Key: "contacts", Value: "planilha ou modelo desconhecido"})
	}
	if template, err := t.deps.Templates.Get(cc.WorkspaceID, req.TemplateID); err == nil {
		fields = append(fields, copilot.Field{Key: "template", Value: template.Name})
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return append(fields, copilot.Field{Key: "contacts", Value: "não foi possível ler a planilha"})
	}
	return append(fields,
		copilot.Field{Key: "contacts", Value: fmt.Sprintf("%d de %d linhas", preview.ValidRows, preview.TotalRows)},
		copilot.Field{Key: "estimatedCost", Value: formatUSD(preview.CostMicros)},
		copilot.Field{Key: "balance", Value: formatUSD(preview.BalanceMicros)},
	)
}

func phoneLabel(phones businessphone.WorkspacePhonesUseCase, cc copilot.Context, raw string) string {
	id := strings.TrimSpace(raw)
	out, err := phones.List(cc.WorkspaceID, businessphone.ListInput{Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: catalogPageSize}}})
	if err == nil {
		for _, p := range out.Items {
			if p != nil && p.ID == id {
				return p.DisplayPhoneNumber
			}
		}
	}
	return "número desconhecido"
}

func (t *createCampaignTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createCampaignArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	req, err := a.request(cc)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	phoneID, err := knownID(a.BusinessPhoneID, "business_phone_id", "list_business_phones")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	scoped, err := creationContext(ctx, a.DepartmentID)
	if err != nil {
		return campaignFailure(err)
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return campaignFailure(err)
	}
	if preview.ValidRows == 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "a planilha não tem nenhuma linha válida; rode preview_campaign_import e corrija"}
	}
	created, err := t.deps.Create.Execute(scoped, &wc.Campaign{
		WorkspaceID:     cc.WorkspaceID,
		Name:            a.Name,
		Type:            wc.CampaignTypeStandard,
		TemplateID:      req.TemplateID,
		BusinessPhoneID: phoneID,
		PhoneInputs:     preview.Rows,
	})
	if err != nil {
		return campaignFailure(err)
	}
	data := map[string]interface{}{"campaign_id": created.ID, "name": created.Name, "contacts": preview.ValidRows, "skipped_rows": preview.TotalRows - preview.ValidRows}
	if created.Metrics != nil {
		data["contacts"] = created.Metrics.TotalNumbers
		data["blocked_by_spam_protection"] = created.Metrics.NotEligiblePossibleSpam
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

type startCampaignArgs struct {
	CampaignID string `json:"campaign_id" req:"true" desc:"campaign_id exato de create_campaign ou campaign_dispatch" id:"true"`
}

type startCampaignTool struct{ deps CampaignDeps }

func NewStartCampaignTool(deps CampaignDeps) copilot.Tool { return &startCampaignTool{deps: deps} }

func (t *startCampaignTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionStart}
}

func (t *startCampaignTool) Definition() tools.Definition {
	return definition("start_campaign",
		"Inicia o envio de uma campanha do WhatsApp oficial. Cada mensagem é cobrada do saldo: a aprovação mostra o custo final "+
			"e o saldo. Nunca inicie sem o usuário pedir explicitamente. Só depois da aprovação do usuário.", startCampaignArgs{})
}

func (t *startCampaignTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a startCampaignArgs
	bindArgs(args, &a)
	campaign, err := t.owned(cc, a.CampaignID)
	if err != nil {
		return []copilot.Field{{Key: "campaign", Value: "campanha desconhecida ou sem acesso"}}
	}
	fields := []copilot.Field{{Key: "campaign", Value: campaign.Name}}
	pending := int64(0)
	if campaign.Metrics != nil {
		pending = campaign.Metrics.Pending
	}
	fields = append(fields, copilot.Field{Key: "contacts", Value: fmt.Sprintf("%d", pending)})
	if template, err := t.deps.Templates.Get(cc.WorkspaceID, campaign.TemplateID); err == nil {
		fields = append(fields, copilot.Field{Key: "template", Value: template.Name}, copilot.Field{Key: "finalCost", Value: t.cost(cc, template, pending)})
	}
	if balance, err := t.deps.Balance.GetBalance(cc.WorkspaceID); err == nil {
		fields = append(fields, copilot.Field{Key: "balance", Value: formatUSD(balance)})
	}
	return fields
}

func (t *startCampaignTool) cost(cc copilot.Context, template *tmpl.Template, contacts int64) string {
	category, err := template.BillingCategory()
	if err != nil {
		return "indisponível"
	}
	unit, err := t.deps.Costs.GetTemplateCostMicros(cc.WorkspaceID, category)
	if err != nil || unit <= 0 {
		return "indisponível"
	}
	return formatUSD(unit * contacts)
}

func (t *startCampaignTool) owned(cc copilot.Context, raw string) (*wc.Campaign, error) {
	id, err := knownID(raw, "campaign_id", "create_campaign")
	if err != nil {
		return nil, err
	}
	return t.deps.Access.Owned(cc.WorkspaceID, cc.Departments, id)
}

func (t *startCampaignTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a startCampaignArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := knownID(a.CampaignID, "campaign_id", "create_campaign")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	campaign, err := t.deps.Start.Start(cc.WorkspaceID, cc.Departments, id)
	if err != nil {
		return campaignFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"started": true, "campaign_id": campaign.ID, "name": campaign.Name}}
}

func creationContext(ctx context.Context, departmentID string) (context.Context, error) {
	scope, ok := wd.GetCreationScope(ctx)
	if !ok || strings.TrimSpace(scope.UserID) == "" {
		return nil, wd.ErrDepartmentAccessDenied
	}
	if id := strings.TrimSpace(departmentID); id != "" {
		known, err := knownID(id, "department_id", "list_departments")
		if err != nil {
			return nil, err
		}
		scope.RequestedDepartmentID = known
	}
	return wd.WithCreationScope(ctx, scope), nil
}

func formatUSD(micros int64) string {
	return fmt.Sprintf("US$ %.2f", float64(micros)/1_000_000)
}

func campaignFailure(err error) copilot.Result {
	if res, ok := departmentFailure(err); ok {
		return res
	}
	switch {
	case errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, media.ErrMediaNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "anexo desconhecido; peça ao usuário para anexar a planilha na conversa"}
	case errors.Is(err, media.ErrMediaTooLarge):
		return copilot.Result{Status: copilot.StatusError, Message: "a planilha é grande demais; importe pela tela de campanhas"}
	case errors.Is(err, tmpl.ErrTemplateAccessDenied), errors.Is(err, tmpl.ErrTemplateNotFound), errors.Is(err, wc.ErrCampaignTemplateNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "modelo desconhecido ou sem acesso; use os ids de list_templates"}
	case errors.Is(err, wc.ErrImportEmpty):
		return copilot.Result{Status: copilot.StatusError, Message: "a planilha está vazia ou só tem o cabeçalho"}
	case errors.Is(err, wc.ErrImportNumberColumn):
		return copilot.Result{Status: copilot.StatusError, Message: "coluna de telefone não encontrada; passe number_column com o nome exato do cabeçalho"}
	case errors.Is(err, wc.ErrImportVariableCount):
		return copilot.Result{Status: copilot.StatusError, Message: "passe variable_columns com uma coluna existente para cada variável do modelo, na ordem"}
	case errors.Is(err, wc.ErrCampaignPhoneNumbersTooMany):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("a planilha passa do limite de %d contatos por campanha", wc.MaxCampaignPhoneNumbers)}
	case errors.Is(err, wc.ErrCampaignBusinessPhoneNotFound), errors.Is(err, wc.ErrCampaignBusinessPhoneNoAccess):
		return copilot.Result{Status: copilot.StatusDenied, Message: "esse número não é deste workspace; use list_business_phones"}
	case errors.Is(err, wc.ErrCampaignTemplatePhoneMismatch):
		return copilot.Result{Status: copilot.StatusError, Message: "o modelo é de outra conta do WhatsApp, diferente do número escolhido"}
	case errors.Is(err, wc.ErrCampaignTemplateNotApproved), errors.Is(err, wc.ErrCampaignTemplateNamedParameters):
		return copilot.Result{Status: copilot.StatusError, Message: "esse modelo não pode ser usado em campanha: " + err.Error()}
	case errors.Is(err, wc.ErrCampaignNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "campanha desconhecida ou sem acesso"}
	case errors.Is(err, wc.ErrCampaignNoSubscription):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o workspace não tem assinatura ativa; campanhas não podem ser iniciadas"}
	case errors.Is(err, wc.ErrCampaignNoNumbers), errors.Is(err, wc.ErrCampaignAllProcessed):
		return copilot.Result{Status: copilot.StatusError, Message: "a campanha não tem contatos pendentes para enviar"}
	case errors.Is(err, wc.ErrDispatchCampaignAlreadyRunning):
		return copilot.Result{Status: copilot.StatusError, Message: "a campanha já está em envio"}
	}
	var notReady *wc.TemplateNotReadyError
	if errors.As(err, &notReady) {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	log.Printf("[copilot] campaign tool failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha na operação da campanha"}
}

func requirePhone(phones businessphone.WorkspacePhonesUseCase, cc copilot.Context, raw string) error {
	id := strings.TrimSpace(raw)
	out, err := phones.List(cc.WorkspaceID, businessphone.ListInput{Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: catalogPageSize}}})
	if err != nil {
		return err
	}
	if len(out.Items) == 0 {
		return fmt.Errorf("%w: o workspace não tem nenhum número do WhatsApp oficial conectado; explique ao usuário que ele precisa conectar um número primeiro", errInvalidArgs)
	}
	for _, p := range out.Items {
		if p != nil && p.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%w: esse número não é deste workspace; use o business_phone_id exato de list_business_phones", errInvalidArgs)
}

func (t *createCampaignTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createCampaignArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := requirePhone(t.deps.Phones, cc, a.BusinessPhoneID); err != nil {
		return err
	}
	req, err := a.request(cc)
	if err != nil {
		return err
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, campaignFailure(err).Message)
	}
	if preview.ValidRows == 0 {
		return fmt.Errorf("%w: a planilha não tem nenhuma linha válida; rode preview_campaign_import e corrija", errInvalidArgs)
	}
	return nil
}

func (t *startCampaignTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[startCampaignArgs](nil, cc, args)
	if err != nil {
		return err
	}
	campaign, err := t.owned(cc, a.CampaignID)
	if err != nil {
		return fmt.Errorf("%w: campanha desconhecida ou sem acesso", errInvalidArgs)
	}
	if err := campaign.Startable(); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, campaignFailure(err).Message)
	}
	return nil
}

func (t *createCampaignTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a createCampaignArgs
	bindArgs(args, &a)
	req, err := a.request(cc)
	if err != nil {
		return nil
	}
	template, err := t.deps.Templates.Get(cc.WorkspaceID, req.TemplateID)
	if err != nil || template == nil {
		return nil
	}
	var variables []string
	if preview, err := t.deps.Preview.Preview(ctx, req); err == nil && len(preview.Rows) > 0 {
		variables = preview.Rows[0].Variables
	}
	return &copilot.Preview{Kind: copilot.PreviewWhatsAppTemplate, Data: templatePreviewOf(template, variables)}
}
