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
	CurrentDealForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error)
	ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
}

func dealPipelinePicker() workflow.ConfigField {
	return workflow.ConfigField{
		Key:           "pipeline_id",
		Label:         "Funil de negócios",
		Type:          "select",
		OptionsSource: "opportunity_pipelines",
		Required:      true,
		Description:   "O funil de oportunidades onde fica o negócio desta conversa.",
	}
}

func dealStagePicker(description string) workflow.ConfigField {
	return workflow.ConfigField{
		Key:           "stage_id",
		Label:         "Etapa do negócio",
		Type:          "select",
		OptionsSource: "opportunity_stages",
		Description:   description,
	}
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
		Label:       "Gerenciar Negócio",
		Description: "Cria ou atualiza o negócio (oportunidade) da conversa: valor, etapa, ganho ou perda.",
		Icon:        "CurrencyDollar",
		Guidance: workflow.NodeGuidance{
			When: "Para registrar a venda da conversa no funil de negócios, por exemplo depois que um agente confirma o fechamento.",
			Behavior: "A conversa tem no máximo um negócio aberto por funil: criar de novo atualiza o mesmo negócio. " +
				"O fluxo fica registrado como responsável e autor. Ganho exige valor; perdido exige motivo.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o negócio foi registrado"},
			{Key: "created", Description: "true quando o negócio foi criado agora"},
			{Key: "opportunity_id", Description: "ID do negócio"},
			{Key: "status", Description: "Situação do negócio: open, won ou lost"},
			{Key: "stage_id", Description: "ID da etapa do negócio"},
			{Key: "title", Description: "Título do negócio"},
			{Key: "value_cents", Description: "Valor em centavos"},
			{Key: "value", Description: "Valor na unidade da moeda"},
			{Key: "currency", Description: "Moeda do negócio"},
			{Key: "error", Description: "Motivo quando o negócio não pôde ser registrado"},
		},
		DefaultConfig: map[string]interface{}{
			"pipeline_id": "",
			"action":      string(opportunity_usecase.EntryCreate),
			"title":       "Negócio da conversa",
			"value":       "",
		},
		ConfigSchema: []workflow.ConfigField{
			dealPipelinePicker(),
			{Key: "action", Label: "Ação", Type: "select", Options: actions, Required: true},
			dealStagePicker("Etapa destino. Obrigatória para mover; nas outras ações é opcional."),
			{Key: "title", Label: "Título", Type: "text", Placeholder: "Plano Pro", Description: "Título usado quando o negócio é criado."},
			{Key: "value", Label: "Valor", Type: "text", Placeholder: "{{last.value}}", Description: "Número com ponto decimal, ex.: 1500.50. Aceita variáveis."},
			{Key: "lost_reason", Label: "Motivo da perda", Type: "text", Description: "Obrigatório para marcar como perdido. Aceita variáveis."},
		},
	}
}

var dealActionLabels = map[opportunity_usecase.EntryAction]string{
	opportunity_usecase.EntryCreate:      "Criar ou atualizar",
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
		return failDeal(edges, "negócios indisponíveis neste contexto (ex.: simulação)"), nil
	}

	cmd := opportunity_usecase.EntryCommand{
		EntryID:      ctx.Run.EntryID,
		EntryType:    ctx.Run.EntryType,
		PipelineID:   pipelineID,
		Actor:        workflowActor(ctx.Run.WorkflowID),
		Action:       action,
		Title:        nodeText(ctx, "title"),
		StageID:      nodeText(ctx, "stage_id"),
		LostReasonID: nodeText(ctx, "lost_reason"),
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
	opportunity_usecase.RefusalWonWithoutValue:     "negócio ganho precisa de valor: preencha o campo Valor do nó",
	opportunity_usecase.RefusalNoOpenDeal:          "a conversa não tem negócio aberto para marcar como perdido",
	opportunity_usecase.RefusalLostReasonMissing:   "negócio perdido precisa de motivo: preencha o campo Motivo da perda do nó",
	opportunity_usecase.RefusalTitleMissing:        "preencha o Título do negócio no nó",
	opportunity_usecase.RefusalStageInvalid:        "a etapa escolhida não é do funil do nó: escolha outra no campo Etapa do negócio",
	opportunity_usecase.RefusalAmountInvalid:       "valor inválido: use um número positivo com ponto decimal, ex.: 1500.50",
	opportunity_usecase.RefusalCurrencyUnsupported: "a moeda do negócio não é suportada",
	opportunity_usecase.RefusalPipelineInvalid:     "o funil escolhido não existe mais ou não é um funil de negócios completo (aberto, ganho e perdido)",
	opportunity_usecase.RefusalRequiredFields:      "o funil exige campos personalizados obrigatórios que o fluxo não preenche",
}

func dealFailure(err error) string {
	if refusal, ok := opportunity_usecase.RefusalOf(err); ok {
		return nodeRefusals[refusal]
	}
	return fmt.Sprintf("falha ao registrar o negócio: %v", err)
}

const (
	dealCheckExists = "exists"
	dealCheckStatus = "status"
	dealCheckStage  = "stage"
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
		Label:       "Verificar Negócio",
		Description: "Verifica o negócio da conversa: se existe, sua situação ou sua etapa.",
		Icon:        "CurrencyDollar",
		Guidance: workflow.NodeGuidance{
			When:     "Para seguir caminhos diferentes conforme a conversa já tenha um negócio, ele esteja ganho ou em uma etapa.",
			Behavior: "Considera o negócio aberto da conversa no funil escolhido ou, sem um aberto, o último fechado.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "true", Label: "Sim"},
			{ID: "false", Label: "Não"},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "true quando a verificação foi atendida"},
			{Key: "opportunity_id", Description: "ID do negócio (vazio se não há)"},
			{Key: "status", Description: "Situação do negócio: open, won ou lost"},
			{Key: "stage_id", Description: "ID da etapa do negócio"},
			{Key: "value_cents", Description: "Valor em centavos"},
			{Key: "value", Description: "Valor na unidade da moeda"},
		},
		DefaultConfig: map[string]interface{}{"pipeline_id": "", "check": dealCheckExists},
		ConfigSchema: []workflow.ConfigField{
			dealPipelinePicker(),
			{Key: "check", Label: "Verificar", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: dealCheckExists, Label: "Tem negócio"},
				{Value: dealCheckStatus, Label: "Situação do negócio é"},
				{Value: dealCheckStage, Label: "Negócio está na etapa"},
			}},
			{Key: "status", Label: "Situação", Type: "select", Description: "Usado quando verifica a situação.", Options: []workflow.ConfigFieldOption{
				{Value: string(opportunity.StatusOpen), Label: "Aberto"},
				{Value: string(opportunity.StatusWon), Label: "Ganho"},
				{Value: string(opportunity.StatusLost), Label: "Perdido"},
			}},
			dealStagePicker("Usada quando verifica a etapa."),
		},
	}
}

func (e *checkOpportunityExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	pipelineID := nodeText(ctx, "pipeline_id")
	check := nodeText(ctx, "check")
	status := opportunity.Status(nodeText(ctx, "status"))
	stageID := nodeText(ctx, "stage_id")
	if pipelineID == "" || !dealCheckComplete(check, status, stageID) {
		return nil, workflow.ErrNodeConfigMissing
	}

	var deal *opportunity.Opportunity
	if e.deals != nil {
		current, err := e.deals.CurrentDealForEntry(ctx.Run.WorkspaceID, pipelineID, ctx.Run.EntryID, ctx.Run.EntryType)
		switch {
		case errors.Is(err, opportunity.ErrNotFound):
		case err != nil:
			return nil, err
		default:
			deal = current
		}
	}

	out := map[string]interface{}{"matched": false, "opportunity_id": "", "status": "", "stage_id": "", "value_cents": int64(0), "value": float64(0)}
	if deal != nil {
		for key, value := range dealOutput(deal) {
			out[key] = value
		}
		switch check {
		case dealCheckExists:
			out["matched"] = true
		case dealCheckStatus:
			out["matched"] = deal.Status == status
		case dealCheckStage:
			out["matched"] = deal.StageID == stageID
		}
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

func dealCheckComplete(check string, status opportunity.Status, stageID string) bool {
	switch check {
	case dealCheckExists:
		return true
	case dealCheckStatus:
		return status.Valid()
	case dealCheckStage:
		return stageID != ""
	}
	return false
}
