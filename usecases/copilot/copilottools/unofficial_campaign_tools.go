package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/tools"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/workspace"
)

const unofficialNumbersPageSize = 50

var errUnofficialScopeDenied = fmt.Errorf("%w: o usuário não tem acesso aos números do WhatsApp não oficial deste workspace", errInvalidArgs)

type UnofficialCampaignDeps struct {
	Scopes  uw.DepartmentScopeSource
	Numbers uw.ListInstancesUseCase
	Usable  uwc.CampaignInstanceUseCase
	Preview uwc.ImportPreviewUseCase
	Create  uwc.CreateCampaignUseCase
	Access  uwc.CampaignAccessUseCase
	Actions uwc.CampaignActionUseCase
	Media   media.GetMediaUseCase
}

func (d UnofficialCampaignDeps) scope(cc copilot.Context) (uw.DepartmentScope, error) {
	scope, ok := uw.ResolveScope(d.Scopes, cc.UserID, cc.WorkspaceID, cc.SystemAdmin)
	if !ok {
		return uw.DepartmentScope{}, errUnofficialScopeDenied
	}
	return scope, nil
}

type listUnofficialNumbersTool struct{ deps UnofficialCampaignDeps }

func NewListUnofficialNumbersTool(deps UnofficialCampaignDeps) copilot.Tool {
	return &listUnofficialNumbersTool{deps: deps}
}

func (t *listUnofficialNumbersTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceUnofficialWhatsAppInstances, Action: workspace.ActionRead}
}

func (t *listUnofficialNumbersTool) Definition() tools.Definition {
	return definition("list_unofficial_numbers",
		"Lista os números do WhatsApp não oficial (conectados por QR code) que o usuário vê, com o status de cada um. "+
			"Use o number_id exato em create_unofficial_campaign.", struct{}{})
}

func (t *listUnofficialNumbersTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	scope, err := t.deps.scope(cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	out, err := t.deps.Numbers.Execute(ctx, uw.ListInstancesInput{
		WorkspaceID: cc.WorkspaceID,
		Scope:       scope,
		Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: unofficialNumbersPageSize}},
	})
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	numbers := make([]map[string]interface{}, 0, len(out.Items))
	for _, i := range out.Items {
		if i == nil {
			continue
		}
		_, sendErr := i.CanSend(time.Now().UTC())
		numbers = append(numbers, map[string]interface{}{
			"number_id": i.ID, "name": i.Label(), "phone": i.PhoneNumber, "status": i.Status, "can_send_now": sendErr == nil,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"numbers": numbers}}
}

type unofficialMessageArgs struct {
	sheetArgs
	Message           string `json:"message" desc:"o texto exato que cada contato recebe; variáveis {{1}}, {{2}} na ordem de variable_columns (legenda, quando houver anexo)"`
	AttachmentMediaID string `json:"attachment_media_id" id:"true" desc:"media_id de um anexo da conversa (imagem, vídeo, áudio ou documento) para enviar junto"`
}

func (a unofficialMessageArgs) request(deps UnofficialCampaignDeps, cc copilot.Context) (uwc.ImportRequest, error) {
	mediaID, mapping, err := a.sheet()
	if err != nil {
		return uwc.ImportRequest{}, err
	}
	message, err := a.message(deps, cc)
	if err != nil {
		return uwc.ImportRequest{}, err
	}
	return uwc.ImportRequest{WorkspaceID: cc.WorkspaceID, MediaID: mediaID, Message: message, Mapping: mapping}, nil
}

func (a unofficialMessageArgs) message(deps UnofficialCampaignDeps, cc copilot.Context) (uwc.MessageSpec, error) {
	spec := uwc.MessageSpec{Kind: uwc.KindText}
	if text := strings.TrimSpace(a.Message); text != "" {
		spec.Bodies = []string{text}
	}
	id := strings.TrimSpace(a.AttachmentMediaID)
	if id == "" {
		return spec, nil
	}
	attachment, err := deps.Media.GetMedia(cc.WorkspaceID, id)
	if err != nil {
		return spec, fmt.Errorf("%w: anexo desconhecido; peça ao usuário para anexar o arquivo na conversa", errInvalidArgs)
	}
	spec.Kind = uwc.KindForMedia(attachment.Type)
	spec.MediaID = attachment.ID
	spec.FileName = attachment.DisplayName()
	return spec, nil
}

type previewUnofficialImportTool struct{ deps UnofficialCampaignDeps }

func NewPreviewUnofficialImportTool(deps UnofficialCampaignDeps) copilot.Tool {
	return &previewUnofficialImportTool{deps: deps}
}

func (t *previewUnofficialImportTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceUnofficialWhatsAppCampaigns, Action: workspace.ActionCreate}
}

func (t *previewUnofficialImportTool) Definition() tools.Definition {
	return definition("preview_unofficial_campaign_import",
		"Simula a importação de uma planilha para uma campanha do WhatsApp não oficial, sem criar nada: confere a mensagem, "+
			"liga as colunas às variáveis {{1}}, {{2}} e aponta telefones inválidos, variáveis faltando e linhas repetidas, com o "+
			"número da linha. Não há custo por mensagem. Use antes de create_unofficial_campaign.", unofficialMessageArgs{})
}

func (t *previewUnofficialImportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a unofficialMessageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	req, err := a.request(t.deps, cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: importData(preview.ImportResult, req.Message)}
}

func importData(result campaign.ImportResult, message uwc.MessageSpec) map[string]interface{} {
	sample := make([]string, 0, campaignSampleRows)
	for i, row := range result.Rows {
		if i == campaignSampleRows {
			break
		}
		sample = append(sample, message.Render(0, row.Variables))
	}
	return map[string]interface{}{
		"headers":      result.Headers,
		"mapping":      result.Mapping,
		"variables":    result.Variables,
		"total_rows":   result.TotalRows,
		"valid_rows":   result.ValidRows,
		"issue_counts": result.IssueCounts,
		"issues":       result.Issues,
		"sample":       sample,
	}
}

type createUnofficialCampaignArgs struct {
	unofficialMessageArgs
	Name         string `json:"name" req:"true" desc:"nome da campanha"`
	NumberID     string `json:"number_id" req:"true" id:"true" desc:"number_id exato de list_unofficial_numbers"`
	DepartmentID string `json:"department_id" id:"true" desc:"departamento (list_departments); só quando o usuário tem mais de um"`
}

type createUnofficialCampaignTool struct{ deps UnofficialCampaignDeps }

func NewCreateUnofficialCampaignTool(deps UnofficialCampaignDeps) copilot.Tool {
	return &createUnofficialCampaignTool{deps: deps}
}

func (t *createUnofficialCampaignTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceUnofficialWhatsAppCampaigns, Action: workspace.ActionCreate}
}

func (t *createUnofficialCampaignTool) Definition() tools.Definition {
	return definition("create_unofficial_campaign",
		"Cria uma campanha do WhatsApp não oficial com as linhas válidas da planilha anexada. A campanha nasce parada; para "+
			"enviar use start_unofficial_campaign. Só depois da aprovação do usuário.", createUnofficialCampaignArgs{})
}

func (t *createUnofficialCampaignTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createUnofficialCampaignArgs](nil, cc, args)
	if err != nil {
		return err
	}
	scope, err := t.deps.scope(cc)
	if err != nil {
		return err
	}
	if _, err := t.deps.Usable.Usable(ctx, cc.WorkspaceID, scope, a.NumberID); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	req, err := a.request(t.deps, cc)
	if err != nil {
		return err
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	if preview.ValidRows == 0 {
		return fmt.Errorf("%w: a planilha não tem nenhuma linha válida; rode preview_unofficial_campaign_import e corrija", errInvalidArgs)
	}
	return nil
}

func (t *createUnofficialCampaignTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createUnofficialCampaignArgs
	bindArgs(args, &a)
	fields := []copilot.Field{
		{Key: "name", Value: strings.TrimSpace(a.Name)},
		{Key: "phone", Value: t.numberLabel(ctx, cc, a.NumberID)},
	}
	req, err := a.request(t.deps, cc)
	if err != nil {
		return fields
	}
	if preview, err := t.deps.Preview.Preview(ctx, req); err == nil {
		fields = append(fields, copilot.Field{Key: "contacts", Value: fmt.Sprintf("%d de %d linhas", preview.ValidRows, preview.TotalRows)})
	}
	if req.Message.MediaID != "" {
		fields = append(fields, copilot.Field{Key: "file", Value: req.Message.FileName})
	}
	return fields
}

func (t *createUnofficialCampaignTool) numberLabel(ctx context.Context, cc copilot.Context, raw string) string {
	scope, err := t.deps.scope(cc)
	if err != nil {
		return "número desconhecido"
	}
	instance, err := t.deps.Usable.Usable(ctx, cc.WorkspaceID, scope, strings.TrimSpace(raw))
	if err != nil {
		return "número desconhecido ou indisponível"
	}
	return instance.Label()
}

func (t *createUnofficialCampaignTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a createUnofficialCampaignArgs
	bindArgs(args, &a)
	req, err := a.request(t.deps, cc)
	if err != nil {
		return nil
	}
	var variables []string
	if preview, err := t.deps.Preview.Preview(ctx, req); err == nil && len(preview.Rows) > 0 {
		variables = preview.Rows[0].Variables
	}
	return messagePreview(req.Message.Render(0, variables), string(shared.EntryTypeUnofficialWhatsApp), "")
}

func (t *createUnofficialCampaignTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createUnofficialCampaignArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	scope, err := t.deps.scope(cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	scoped, err := creationContext(ctx, a.DepartmentID)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	req, err := a.request(t.deps, cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	preview, err := t.deps.Preview.Preview(ctx, req)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	if preview.ValidRows == 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "a planilha não tem nenhuma linha válida; rode preview_unofficial_campaign_import e corrija"}
	}
	created, err := t.deps.Create.Execute(scoped, &uwc.Campaign{
		WorkspaceID: cc.WorkspaceID,
		InstanceID:  strings.TrimSpace(a.NumberID),
		CreatedByID: cc.UserID,
		Name:        a.Name,
		Message:     req.Message,
		Targets:     preview.Targets(),
	}, scope)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	data := map[string]interface{}{"campaign_id": created.ID, "name": created.Name, "contacts": preview.ValidRows, "skipped_rows": preview.TotalRows - preview.ValidRows}
	if created.Metrics != nil {
		data["contacts"] = created.Metrics.TotalNumbers
		data["blocked_by_spam_protection"] = created.Metrics.NotEligiblePossibleSpam
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

type startUnofficialCampaignArgs struct {
	CampaignID string `json:"campaign_id" req:"true" id:"true" desc:"campaign_id exato de create_unofficial_campaign"`
}

type startUnofficialCampaignTool struct{ deps UnofficialCampaignDeps }

func NewStartUnofficialCampaignTool(deps UnofficialCampaignDeps) copilot.Tool {
	return &startUnofficialCampaignTool{deps: deps}
}

func (t *startUnofficialCampaignTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceUnofficialWhatsAppCampaigns, Action: workspace.ActionStart}
}

func (t *startUnofficialCampaignTool) Definition() tools.Definition {
	return definition("start_unofficial_campaign",
		"Inicia o envio de uma campanha do WhatsApp não oficial. As mensagens saem aos poucos, no ritmo seguro do número, "+
			"para evitar bloqueio. Nunca inicie sem o usuário pedir explicitamente. Só depois da aprovação do usuário.",
		startUnofficialCampaignArgs{})
}

func (t *startUnofficialCampaignTool) owned(ctx context.Context, cc copilot.Context, raw string) (*uwc.Campaign, uw.DepartmentScope, error) {
	scope, err := t.deps.scope(cc)
	if err != nil {
		return nil, scope, err
	}
	c, err := t.deps.Access.Owned(ctx, cc.WorkspaceID, scope, strings.TrimSpace(raw))
	return c, scope, err
}

func (t *startUnofficialCampaignTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[startUnofficialCampaignArgs](nil, cc, args)
	if err != nil {
		return err
	}
	c, scope, err := t.owned(ctx, cc, a.CampaignID)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	if _, err := campaign.ResolveTransition(c.Status, campaign.ActionStart); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	if err := c.Metrics.Startable(); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	instance, err := t.deps.Usable.Usable(ctx, cc.WorkspaceID, scope, c.InstanceID)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	return nil
}

func (t *startUnofficialCampaignTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a startUnofficialCampaignArgs
	bindArgs(args, &a)
	c, _, err := t.owned(ctx, cc, a.CampaignID)
	if err != nil {
		return []copilot.Field{{Key: "campaign", Value: "campanha desconhecida ou sem acesso"}}
	}
	pending := int64(0)
	if c.Metrics != nil {
		pending = c.Metrics.Pending
	}
	minMS, maxMS := c.SendDelayRange()
	fields := []copilot.Field{
		{Key: "campaign", Value: c.Name},
		{Key: "phone", Value: c.InstanceLabel},
		{Key: "contacts", Value: fmt.Sprintf("%d", pending)},
		{Key: "pace", Value: fmt.Sprintf("uma mensagem a cada %d a %d segundos", minMS/1000, maxMS/1000)},
	}
	if c.DailyCap > 0 {
		fields = append(fields, copilot.Field{Key: "dailyCap", Value: fmt.Sprintf("até %d por dia", c.DailyCap)})
	}
	return fields
}

func (t *startUnofficialCampaignTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a startUnofficialCampaignArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	scope, err := t.deps.scope(cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	c, err := t.deps.Actions.Act(ctx, cc.WorkspaceID, scope, strings.TrimSpace(a.CampaignID), campaign.ActionStart)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"started": true, "campaign_id": c.ID, "name": c.Name}}
}

func unofficialCampaignFailure(err error) copilot.Result {
	if res, ok := importFailure(err); ok {
		return res
	}
	var unusable *uwc.InstanceUnusableError
	switch {
	case errors.Is(err, uw.ErrInstanceNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "número desconhecido ou de outro departamento; use o number_id de list_unofficial_numbers"}
	case errors.As(err, &unusable):
		return copilot.Result{Status: copilot.StatusError, Message: "esse número não pode fazer campanhas: " + unusable.Reason}
	case errors.Is(err, uw.ErrInstanceNotConnected):
		return copilot.Result{Status: copilot.StatusError, Message: "o número está desconectado; peça ao usuário para reconectar pelo QR code"}
	case errors.Is(err, uw.ErrRestrictedByWA):
		return copilot.Result{Status: copilot.StatusError, Message: "o WhatsApp está limitando conversas novas desse número agora; tente mais tarde"}
	case errors.Is(err, uwc.ErrCampaignNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "campanha desconhecida ou sem acesso"}
	case errors.Is(err, uwc.ErrCampaignTargetsTooMany):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("a planilha passa do limite de %d contatos por campanha", uwc.MaxCampaignTargets)}
	case errors.Is(err, uwc.ErrMessageBodyRequired), errors.Is(err, uwc.ErrMessageBodyEmpty), errors.Is(err, uwc.ErrMessageBodyTooLong),
		errors.Is(err, uwc.ErrMessageMediaRequired), errors.Is(err, uwc.ErrCampaignVariablesMismatch), errors.Is(err, uwc.ErrCampaignVariableEmpty):
		return copilot.Result{Status: copilot.StatusError, Message: "a mensagem não pode ser enviada assim: " + err.Error()}
	}
	log.Printf("[copilot] unofficial campaign tool failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha na operação da campanha"}
}
