package node_executors

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/opportunity"
	"vozko/domain/workflow"
	opportunity_usecase "vozko/usecases/opportunity"
)

type DealDesk interface {
	DealsForEntry(workspaceID, pipelineID, entryID, entryType string) (opportunity.EntryDeals, error)
	ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
}

func dealPipelinePicker() workflow.ConfigField {
	return workflow.ConfigField{
		Key:           "pipeline_id",
		Label:         "Funil de oportunidades",
		Type:          "select",
		OptionsSource: "opportunity_pipelines",
		Required:      true,
		Description:   "O funil de oportunidades onde fica a oportunidade desta conversa.",
	}
}

func dealStagePicker(description string) workflow.ConfigField {
	return workflow.ConfigField{
		Key:           "stage_id",
		Label:         "Etapa da oportunidade",
		Type:          "select",
		OptionsSource: "opportunity_stages",
		Description:   description,
	}
}

func dealIDField(description string) workflow.ConfigField {
	return workflow.ConfigField{
		Key:         "opportunity_id",
		Label:       "ID da oportunidade",
		Type:        "text",
		Placeholder: "{{no_anterior.opportunity_id}}",
		Description: description,
	}
}

func whenAction(actions ...opportunity_usecase.EntryAction) *workflow.FieldRule {
	values := make([]string, 0, len(actions))
	for _, action := range actions {
		values = append(values, string(action))
	}
	return &workflow.FieldRule{Field: "action", Values: values}
}

func whenCheck(checks ...string) *workflow.FieldRule {
	return &workflow.FieldRule{Field: "check", Values: checks}
}

func withRules(field workflow.ConfigField, visible, required *workflow.FieldRule) workflow.ConfigField {
	field.VisibleWhen = visible
	field.RequiredWhen = required
	return field
}

func nodeText(ctx *workflow.NodeContext, key string) string {
	raw, _ := ctx.Node.Config[key].(string)
	return strings.TrimSpace(workflow.Interpolate(raw, ctx.State, nil))
}

func dealOutput(deal *opportunity.Opportunity) map[string]interface{} {
	return map[string]interface{}{
		"opportunity_id": deal.ID,
		"status":         string(deal.Status),
		"stage_id":       deal.StageID,
		"title":          deal.Title,
		"value_cents":    deal.ValueCents,
		"value":          float64(deal.ValueCents) / 100,
		"currency":       deal.Currency,
	}
}

type manageOpportunityExecutor struct {
	deals DealDesk
}

func NewManageOpportunityExecutor(deals DealDesk) workflow.NodeExecutor {
	return &manageOpportunityExecutor{deals: deals}
}

func (e *manageOpportunityExecutor) Definition() workflow.NodeDefinition {
	actions := make([]workflow.ConfigFieldOption, 0, len(opportunity_usecase.EntryActions()))
	for _, action := range opportunity_usecase.EntryActions() {
		actions = append(actions, workflow.ConfigFieldOption{Value: string(action), Label: dealActionLabels[action]})
	}
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionManageOpportunity,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Gerenciar Oportunidade",
		Description: "Cria ou atualiza oportunidades da conversa: valor, etapa, ganho ou perda.",
		Icon:        "CurrencyDollar",
		Guidance: workflow.NodeGuidance{
			When: "Para registrar a venda da conversa no funil de oportunidades, por exemplo depois que um agente confirma o fechamento.",
			Behavior: "Sem ID da oportunidade, o nó age na única oportunidade aberta da conversa neste funil; com mais de uma aberta, segue pela saída Erro. " +
				"Criar nova oportunidade sempre abre outra, para quando a conversa trata mais de um contrato. " +
				"Mover, atualizar valor e marcar como perdido nunca criam oportunidade. O fluxo fica registrado como responsável e autor. " +
				"Ganho exige valor; perdido exige motivo.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a oportunidade foi registrada"},
			{Key: "created", Description: "true quando a oportunidade foi criada agora"},
			{Key: "opportunity_id", Description: "ID da oportunidade"},
			{Key: "status", Description: "Situação da oportunidade: open, won ou lost"},
			{Key: "stage_id", Description: "ID da etapa da oportunidade"},
			{Key: "title", Description: "Título da oportunidade"},
			{Key: "value_cents", Description: "Valor em centavos"},
			{Key: "value", Description: "Valor na unidade da moeda"},
			{Key: "currency", Description: "Moeda da oportunidade"},
			{Key: "error", Description: "Motivo quando a oportunidade não pôde ser registrada"},
		},
		DefaultConfig: map[string]interface{}{
			"pipeline_id": "",
			"action":      string(opportunity_usecase.EntryCreate),
			"title":       "Oportunidade da conversa",
			"value":       "",
		},
		ConfigSchema: []workflow.ConfigField{
			dealPipelinePicker(),
			{Key: "action", Label: "Ação", Type: "select", Options: actions, Required: true},
			withRules(dealIDField("Opcional. Vazio: a única oportunidade aberta da conversa neste funil. Com mais de uma aberta, informe o ID vindo de um passo anterior."),
				whenAction(opportunity_usecase.EntryCreate, opportunity_usecase.EntryUpdateValue, opportunity_usecase.EntryMove, opportunity_usecase.EntryWin, opportunity_usecase.EntryLose), nil),
			withRules(dealStagePicker("Etapa onde a oportunidade fica. Obrigatória para mover."),
				whenAction(opportunity_usecase.EntryCreate, opportunity_usecase.EntryCreateNew, opportunity_usecase.EntryMove), whenAction(opportunity_usecase.EntryMove)),
			withRules(workflow.ConfigField{Key: "title", Label: "Título", Type: "text", Placeholder: "Plano Pro", Description: "Título usado quando a oportunidade é criada."},
				whenAction(opportunity_usecase.EntryCreate, opportunity_usecase.EntryCreateNew), nil),
			withRules(workflow.ConfigField{Key: "value", Label: "Valor", Type: "text", Placeholder: "{{last.value}}", Description: "Número com ponto decimal, ex.: 1500.50. Aceita variáveis. Ganho exige valor quando a oportunidade ainda não tem."},
				whenAction(opportunity_usecase.EntryCreate, opportunity_usecase.EntryCreateNew, opportunity_usecase.EntryUpdateValue, opportunity_usecase.EntryWin), whenAction(opportunity_usecase.EntryUpdateValue)),
			withRules(workflow.ConfigField{Key: "lost_reason", Label: "Motivo da perda", Type: "text", Description: "Aceita variáveis."},
				whenAction(opportunity_usecase.EntryLose), whenAction(opportunity_usecase.EntryLose)),
		},
	}
}

var dealActionLabels = map[opportunity_usecase.EntryAction]string{
	opportunity_usecase.EntryCreate:      "Criar ou atualizar",
	opportunity_usecase.EntryCreateNew:   "Criar nova oportunidade",
	opportunity_usecase.EntryUpdateValue: "Atualizar valor",
	opportunity_usecase.EntryMove:        "Mover de etapa",
	opportunity_usecase.EntryWin:         "Marcar como ganho",
	opportunity_usecase.EntryLose:        "Marcar como perdido",
}

func (e *manageOpportunityExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	pipelineID := nodeText(ctx, "pipeline_id")
	action := opportunity_usecase.EntryAction(nodeText(ctx, "action"))
	if pipelineID == "" || !action.Valid() {
		return nil, workflow.ErrNodeConfigMissing
	}
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	if e.deals == nil {
		return failDeal(edges, "oportunidades indisponíveis neste contexto (ex.: simulação)"), nil
	}

	cmd := opportunity_usecase.EntryCommand{
		EntryID:       ctx.Run.EntryID,
		EntryType:     ctx.Run.EntryType,
		PipelineID:    pipelineID,
		Actor:         workflowActor(ctx.Run.WorkflowID),
		Action:        action,
		OpportunityID: nodeText(ctx, "opportunity_id"),
		Title:         nodeText(ctx, "title"),
		StageID:       nodeText(ctx, "stage_id"),
		LostReasonID:  nodeText(ctx, "lost_reason"),
	}
	if value := nodeText(ctx, "value"); value != "" {
		cents, err := opportunity.CentsFromText(value)
		if err != nil {
			return failDeal(edges, nodeRefusals[opportunity_usecase.RefusalAmountInvalid]), nil
		}
		cmd.ValueCents = &cents
	}

	result, err := e.deals.ManageForEntry(ctx.Run.WorkspaceID, cmd)
	if err != nil {
		return failDeal(edges, dealFailure(err)), nil
	}
	out := dealOutput(result.Opportunity)
	out["success"] = true
	out["created"] = result.Created
	return &workflow.NodeResult{NextNodeID: resolveEdgeByLabelStrict(edges, "sucesso"), Output: out}, nil
}

func failDeal(edges []workflow.Edge, reason string) *workflow.NodeResult {
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(edges, "erro"),
		Output:     map[string]interface{}{"success": false, "created": false, "error": reason},
	}
}

var nodeRefusals = map[opportunity_usecase.Refusal]string{
	opportunity_usecase.RefusalWonWithoutValue:     "oportunidade ganha precisa de valor: preencha o campo Valor do nó",
	opportunity_usecase.RefusalNoOpenDeal:          "a conversa não tem oportunidade aberta neste funil: use Criar ou atualizar para abrir uma",
	opportunity_usecase.RefusalLostReasonMissing:   "oportunidade perdida precisa de motivo: preencha o campo Motivo da perda do nó",
	opportunity_usecase.RefusalTitleMissing:        "preencha o Título da oportunidade no nó",
	opportunity_usecase.RefusalStageInvalid:        "a etapa escolhida não é do funil do nó: escolha outra no campo Etapa da oportunidade",
	opportunity_usecase.RefusalAmountInvalid:       "valor inválido: use um número positivo com ponto decimal, ex.: 1500.50",
	opportunity_usecase.RefusalCurrencyUnsupported: "a moeda da oportunidade não é suportada",
	opportunity_usecase.RefusalPipelineInvalid:     "o funil escolhido não existe mais ou não é um funil de oportunidades completo (aberto, ganho e perdido)",
	opportunity_usecase.RefusalRequiredFields:      "o funil exige campos personalizados obrigatórios que o fluxo não preenche",
	opportunity_usecase.RefusalAmbiguousDeal:       "a conversa tem mais de uma oportunidade aberta neste funil: informe o ID da oportunidade no nó",
	opportunity_usecase.RefusalDealNotLinked:       "o ID da oportunidade não pertence a esta conversa neste funil",
	opportunity_usecase.RefusalDealClosed:          "a oportunidade informada já foi encerrada",
	opportunity_usecase.RefusalDealIDOnNewDeal:     "Criar nova oportunidade não usa ID da oportunidade: deixe o campo vazio",
	opportunity_usecase.RefusalDealChanged:         "a oportunidade foi alterada ao mesmo tempo por outra pessoa ou automação; nada foi sobrescrito",
}

func dealFailure(err error) string {
	if refusal, ok := opportunity_usecase.RefusalOf(err); ok {
		return nodeRefusals[refusal]
	}
	return fmt.Sprintf("falha ao registrar a oportunidade: %v", err)
}

const (
	dealCheckExists      = "exists"
	dealCheckSeveralOpen = "several_open"
	dealCheckStatus      = "status"
	dealCheckStage       = "stage"
)

type checkOpportunityExecutor struct {
	deals DealDesk
}

func NewCheckOpportunityExecutor(deals DealDesk) workflow.NodeExecutor {
	return &checkOpportunityExecutor{deals: deals}
}

func (e *checkOpportunityExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionCheckOpportunity,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Verificar Oportunidade",
		Description: "Verifica as oportunidades da conversa: se existem, se há mais de uma aberta, a situação ou a etapa.",
		Icon:        "CurrencyDollar",
		Guidance: workflow.NodeGuidance{
			When: "Para seguir caminhos diferentes conforme a conversa já tenha uma oportunidade, ela esteja ganha ou em uma etapa.",
			Behavior: "Considera a oportunidade do ID informado ou a única oportunidade aberta da conversa no funil; sem aberta, a última encerrada. " +
				"Com mais de uma aberta e sem ID, verificar situação ou etapa interrompe a execução. A saída lista todas as oportunidades para usar em um Loop.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "true", Label: "Sim"},
			{ID: "false", Label: "Não"},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "true quando a verificação foi atendida"},
			{Key: "opportunity_id", Description: "ID da oportunidade (vazio se não há)"},
			{Key: "status", Description: "Situação da oportunidade: open, won ou lost"},
			{Key: "stage_id", Description: "ID da etapa da oportunidade"},
			{Key: "value_cents", Description: "Valor em centavos"},
			{Key: "value", Description: "Valor na unidade da moeda"},
			{Key: "open_count", Description: "Quantidade de oportunidades abertas da conversa no funil"},
			{Key: "opportunities", Description: "Lista das oportunidades da conversa no funil, abertas primeiro"},
		},
		DefaultConfig: map[string]interface{}{"pipeline_id": "", "check": dealCheckExists},
		ConfigSchema: []workflow.ConfigField{
			dealPipelinePicker(),
			{Key: "check", Label: "Verificar", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: dealCheckExists, Label: "Tem oportunidade"},
				{Value: dealCheckSeveralOpen, Label: "Tem mais de uma oportunidade aberta"},
				{Value: dealCheckStatus, Label: "Situação da oportunidade é"},
				{Value: dealCheckStage, Label: "Oportunidade está na etapa"},
			}},
			withRules(dealIDField("Opcional. Vazio: a única oportunidade aberta da conversa neste funil ou, sem aberta, a última encerrada."),
				whenCheck(dealCheckExists, dealCheckStatus, dealCheckStage), nil),
			withRules(workflow.ConfigField{Key: "status", Label: "Situação", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: string(opportunity.StatusOpen), Label: "Aberto"},
				{Value: string(opportunity.StatusWon), Label: "Ganho"},
				{Value: string(opportunity.StatusLost), Label: "Perdido"},
			}}, whenCheck(dealCheckStatus), whenCheck(dealCheckStatus)),
			withRules(dealStagePicker("Etapa a comparar."), whenCheck(dealCheckStage), whenCheck(dealCheckStage)),
		},
	}
}

func (e *checkOpportunityExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	pipelineID := nodeText(ctx, "pipeline_id")
	check := nodeText(ctx, "check")
	status := opportunity.Status(nodeText(ctx, "status"))
	stageID := nodeText(ctx, "stage_id")
	dealID := nodeText(ctx, "opportunity_id")
	if pipelineID == "" || !dealCheckComplete(check, status, stageID) {
		return nil, workflow.ErrNodeConfigMissing
	}

	var deals opportunity.EntryDeals
	if e.deals != nil {
		found, err := e.deals.DealsForEntry(ctx.Run.WorkspaceID, pipelineID, ctx.Run.EntryID, ctx.Run.EntryType)
		if err != nil {
			return nil, err
		}
		deals = found
	}

	deal, err := deals.Current(dealID)
	ambiguous := errors.Is(err, opportunity.ErrAmbiguousDeal)
	if ambiguous && (check == dealCheckStatus || check == dealCheckStage) {
		return nil, fmt.Errorf("%w: node %q", err, ctx.Node.ID)
	}

	out := dealCheckOutput(deals, deal)
	switch check {
	case dealCheckExists:
		out["matched"] = deal != nil || ambiguous
	case dealCheckSeveralOpen:
		out["matched"] = len(deals.Open()) > 1
	case dealCheckStatus:
		out["matched"] = deal != nil && deal.Status == status
	case dealCheckStage:
		out["matched"] = deal != nil && deal.StageID == stageID
	}

	branch := "false"
	if out["matched"] == true {
		branch = "true"
	}
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), branch),
		Output:     out,
	}, nil
}

func dealCheckOutput(deals opportunity.EntryDeals, deal *opportunity.Opportunity) map[string]interface{} {
	list := make([]map[string]interface{}, 0, len(deals))
	for _, d := range deals {
		list = append(list, dealOutput(d))
	}
	out := map[string]interface{}{
		"matched": false, "opportunity_id": "", "status": "", "stage_id": "", "value_cents": int64(0), "value": float64(0),
		"open_count": len(deals.Open()), "opportunities": list,
	}
	if deal != nil {
		for key, value := range dealOutput(deal) {
			out[key] = value
		}
	}
	return out
}

func dealCheckComplete(check string, status opportunity.Status, stageID string) bool {
	switch check {
	case dealCheckExists, dealCheckSeveralOpen:
		return true
	case dealCheckStatus:
		return status.Valid()
	case dealCheckStage:
		return stageID != ""
	}
	return false
}
