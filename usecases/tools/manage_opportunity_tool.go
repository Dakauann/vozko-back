package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/opportunity"
	"vozko/domain/stage"
	"vozko/domain/tools"
	opportunity_usecase "vozko/usecases/opportunity"
)

const (
	ManageOpportunityToolName  = "manage_opportunity"
	OpportunityPipelinesSource = "opportunity_pipelines"
	readOpportunityAction      = "get"
)

type OpportunityManager interface {
	PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error)
	CurrentDealForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error)
	ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
}

func DefaultOpportunityToolActions() []opportunity_usecase.EntryAction {
	return []opportunity_usecase.EntryAction{
		opportunity_usecase.EntryCreate,
		opportunity_usecase.EntryUpdateValue,
		opportunity_usecase.EntryMove,
		opportunity_usecase.EntryLose,
	}
}

type manageOpportunityTool struct {
	deals OpportunityManager
}

func NewManageOpportunityTool(deals OpportunityManager) tools.Handler {
	if deals == nil {
		return nil
	}
	return &manageOpportunityTool{deals: deals}
}

func (t *manageOpportunityTool) Definition() tools.Definition {
	actionOptions := make([]tools.ConfigParameterOption, 0, len(opportunity_usecase.EntryActions()))
	for _, action := range opportunity_usecase.EntryActions() {
		actionOptions = append(actionOptions, tools.ConfigParameterOption{Value: string(action), Label: entryActionLabels[action]})
	}
	defaults := make([]interface{}, 0, len(DefaultOpportunityToolActions()))
	for _, action := range DefaultOpportunityToolActions() {
		defaults = append(defaults, string(action))
	}

	return tools.Definition{
		Name:               ManageOpportunityToolName,
		DisplayName:        "Gerenciar Negócio da Conversa",
		DisplayDescription: "Cria e atualiza o negócio (oportunidade) desta conversa no funil de vendas: valor, etapa, ganho ou perda. A IA fica como responsável pelo negócio que criar.",
		Description: `Gerencia o negócio (oportunidade de venda) desta conversa. Cada conversa tem no máximo um negócio aberto neste funil; criar de novo atualiza o mesmo negócio.

AÇÕES:
- get: consulta o negócio da conversa (o aberto ou, se não houver, o último fechado).
- create: abre o negócio quando o cliente demonstra intenção real de compra. Informe "title" e, se souber, "value".
- update_value: atualiza o valor negociado em "value".
- move: move o negócio para a etapa "stage".
- win: marca como ganho quando o cliente CONFIRMA a compra. Exige "value" com o valor fechado.
- lose: marca como perdido quando o cliente desiste. Exige "lost_reason".

"value" é um número na moeda do negócio, com ponto decimal (ex.: 1500.50). NUNCA invente valores: use apenas o que foi combinado na conversa.`,
		Parameters: map[string]tools.Parameter{
			"action": {
				Type:        "string",
				Description: "Ação a executar.",
				Enum:        append([]string{readOpportunityAction}, entryActionNames(opportunity_usecase.EntryActions())...),
			},
			"title": {
				Type:        "string",
				Description: "Título curto do negócio, como o produto ou plano negociado.",
			},
			"value": {
				Type:        "number",
				Description: "Valor do negócio na unidade da moeda, com ponto decimal (ex.: 1500.50).",
			},
			"stage": {
				Type:        "string",
				Description: "Nome exato da etapa destino para a ação move.",
			},
			"lost_reason": {
				Type:        "string",
				Description: "Motivo da perda, nas palavras do cliente, para a ação lose.",
			},
		},
		Required:   []string{"action"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentAction,
		ConfigSchema: map[string]tools.ConfigParameter{
			"pipeline_id": {
				Type:               "string",
				Description:        "Funil de negócios onde a IA cria e move o negócio da conversa.",
				DisplayName:        "Funil de negócios",
				DisplayDescription: "Funil de oportunidades onde a IA registra o negócio desta conversa.",
				OptionsSource:      OpportunityPipelinesSource,
				Required:           true,
			},
			"allowed_actions": {
				Type:               "array",
				Description:        "Ações que a IA pode executar no negócio.",
				DisplayName:        "Ações permitidas",
				DisplayDescription: "O que a IA pode fazer com o negócio. Marcar como ganho fica desligado até você permitir.",
				Default:            defaults,
				Options:            actionOptions,
			},
			"currency": {
				Type:               "string",
				Description:        "Moeda dos valores informados pela IA.",
				DisplayName:        "Moeda",
				DisplayDescription: "Moeda dos negócios criados pela IA.",
				Options:            currencyOptions(),
			},
		},
		RequiredConfig: []string{"pipeline_id"},
		RequiresConfig: true,
	}
}

var entryActionLabels = map[opportunity_usecase.EntryAction]string{
	opportunity_usecase.EntryCreate:      "Criar negócio",
	opportunity_usecase.EntryUpdateValue: "Atualizar valor",
	opportunity_usecase.EntryMove:        "Mover de etapa",
	opportunity_usecase.EntryWin:         "Marcar como ganho",
	opportunity_usecase.EntryLose:        "Marcar como perdido",
}

func entryActionNames(actions []opportunity_usecase.EntryAction) []string {
	names := make([]string, 0, len(actions))
	for _, action := range actions {
		names = append(names, string(action))
	}
	return names
}

func currencyOptions() []tools.ConfigParameterOption {
	options := make([]tools.ConfigParameterOption, 0, len(opportunity.SupportedCurrencies()))
	for _, currency := range opportunity.SupportedCurrencies() {
		options = append(options, tools.ConfigParameterOption{Value: currency, Label: currency})
	}
	return options
}

func (t *manageOpportunityTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	def := t.Definition()
	settings := opportunitySettingsFrom(ctx.Config)

	params := make(map[string]tools.Parameter, len(def.Parameters))
	for key, param := range def.Parameters {
		params[key] = param
	}
	action := params["action"]
	action.Enum = append([]string{readOpportunityAction}, entryActionNames(settings.allowedActions())...)
	params["action"] = action

	if !settings.allows(opportunity_usecase.EntryMove) {
		delete(params, "stage")
	} else if ctx.WorkspaceID != "" && settings.pipelineID != "" {
		stages, err := t.deals.PipelineStages(ctx.WorkspaceID, settings.pipelineID)
		if err != nil {
			log.Printf("[ManageOpportunity] no stages for workspace=%s pipeline=%s: %v", ctx.WorkspaceID, settings.pipelineID, err)
		} else {
			stageParam := params["stage"]
			stageParam.Enum = openStageNames(stages)
			params["stage"] = stageParam
		}
	}

	def.Parameters = params
	return def
}

type opportunitySettings struct {
	pipelineID string
	currency   string
	allowed    map[opportunity_usecase.EntryAction]bool
}

func opportunitySettingsFrom(config map[string]interface{}) opportunitySettings {
	settings := opportunitySettings{
		pipelineID: configString(config, "pipeline_id"),
		currency:   configString(config, "currency"),
		allowed:    map[opportunity_usecase.EntryAction]bool{},
	}
	raw, configured := config["allowed_actions"]
	if !configured {
		for _, action := range DefaultOpportunityToolActions() {
			settings.allowed[action] = true
		}
		return settings
	}
	values, _ := raw.([]interface{})
	for _, value := range values {
		name, _ := value.(string)
		if action := opportunity_usecase.EntryAction(strings.TrimSpace(name)); action.Valid() {
			settings.allowed[action] = true
		}
	}
	return settings
}

func (s opportunitySettings) allows(action opportunity_usecase.EntryAction) bool {
	return s.allowed[action]
}

func (s opportunitySettings) allowedActions() []opportunity_usecase.EntryAction {
	out := make([]opportunity_usecase.EntryAction, 0, len(s.allowed))
	for _, action := range opportunity_usecase.EntryActions() {
		if s.allowed[action] {
			out = append(out, action)
		}
	}
	return out
}

func openStageNames(stages []*stage.Stage) []string {
	names := make([]string, 0, len(stages))
	for _, st := range stages {
		if !st.IsWon && !st.IsLost {
			names = append(names, st.Name)
		}
	}
	return names
}

func (t *manageOpportunityTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *manageOpportunityTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID := configString(config, "__workspace_id")
	entryID := configString(config, "__entry_id")
	entryType := configString(config, "__entry_type")
	if workspaceID == "" || entryID == "" || entryType == "" {
		return refuse("Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento."), nil
	}
	actorID := agentActor(ctx, config)
	if actorID == "" {
		return refuse("Esta ferramenta só pode ser usada por um agente de IA identificado."), nil
	}
	settings := opportunitySettingsFrom(config)
	if settings.pipelineID == "" {
		return refuse("A ferramenta está sem funil de negócios configurado. Avise um administrador."), nil
	}

	name, _ := params["action"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == readOpportunityAction {
		return t.describeCurrentDeal(workspaceID, entryID, entryType, settings.pipelineID), nil
	}
	action := opportunity_usecase.EntryAction(name)
	if !action.Valid() {
		return refuse(fmt.Sprintf("Ação inválida: %q. Use uma das ações do enum.", name)), nil
	}
	if !settings.allows(action) {
		return refuse(fmt.Sprintf("A ação %q não está habilitada para este agente.", name)), nil
	}

	stages, err := t.deals.PipelineStages(workspaceID, settings.pipelineID)
	if err != nil {
		return refuse(opportunityRefusal(err)), nil
	}
	cmd := opportunity_usecase.EntryCommand{
		EntryID:      entryID,
		EntryType:    entryType,
		LeadID:       configString(config, "__lead_id"),
		PipelineID:   settings.pipelineID,
		Actor:        actorID,
		Action:       action,
		Title:        paramString(params, "title"),
		Currency:     settings.currency,
		LostReasonID: paramString(params, "lost_reason"),
	}
	if raw, given := params["value"]; given && raw != nil {
		cents, err := centsFromParam(raw)
		if err != nil {
			return refuse("O valor deve ser um número positivo com ponto decimal, por exemplo 1500.50."), nil
		}
		cmd.ValueCents = &cents
	}
	if action == opportunity_usecase.EntryMove {
		target := paramString(params, "stage")
		stageID, found := openStageID(stages, target)
		if !found {
			return refuse(fmt.Sprintf("Etapa %q não encontrada entre as etapas abertas do funil: %s. Para fechar o negócio use win ou lose.",
				target, strings.Join(openStageNames(stages), ", "))), nil
		}
		cmd.StageID = stageID
	}

	result, err := t.deals.ManageForEntry(workspaceID, cmd)
	if err != nil {
		return refuse(opportunityRefusal(err)), nil
	}
	summary := describeDeal(result.Opportunity, stages)
	verb := "atualizado"
	if result.Created {
		verb = "criado"
	}
	return tools.ExecutionResult{
		Result:            fmt.Sprintf("Negócio %s. %s", verb, summary),
		ContextUpdateText: summary,
	}, nil
}

func (t *manageOpportunityTool) describeCurrentDeal(workspaceID, entryID, entryType, pipelineID string) tools.ExecutionResult {
	deal, err := t.deals.CurrentDealForEntry(workspaceID, pipelineID, entryID, entryType)
	if errors.Is(err, opportunity.ErrNotFound) {
		return tools.ExecutionResult{Result: "Esta conversa ainda não tem negócio neste funil."}
	}
	if err != nil {
		return refuse(opportunityRefusal(err))
	}
	stages, err := t.deals.PipelineStages(workspaceID, pipelineID)
	if err != nil {
		return refuse(opportunityRefusal(err))
	}
	return tools.ExecutionResult{Result: describeDeal(deal, stages)}
}

func refuse(message string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: message, IsError: true}
}

func paramString(params map[string]interface{}, key string) string {
	value, _ := params[key].(string)
	return strings.TrimSpace(value)
}

func centsFromParam(raw interface{}) (int64, error) {
	switch value := raw.(type) {
	case float64:
		return opportunity.CentsFromAmount(value)
	case string:
		return opportunity.CentsFromText(value)
	}
	return 0, opportunity.ErrInvalidAmount
}

func openStageID(stages []*stage.Stage, name string) (string, bool) {
	for _, st := range stages {
		if !st.IsWon && !st.IsLost && strings.EqualFold(st.Name, name) {
			return st.ID, true
		}
	}
	return "", false
}

func describeDeal(deal *opportunity.Opportunity, stages []*stage.Stage) string {
	stageName := deal.StageID
	for _, st := range stages {
		if st.ID == deal.StageID {
			stageName = st.Name
		}
	}
	return fmt.Sprintf("Negócio %q na etapa %q, valor %s, status %s.",
		deal.Title, stageName, formatAmount(deal.Currency, deal.ValueCents), dealStatusLabels[deal.Status])
}

var dealStatusLabels = map[opportunity.Status]string{
	opportunity.StatusOpen: "aberto",
	opportunity.StatusWon:  "ganho",
	opportunity.StatusLost: "perdido",
}

func formatAmount(currency string, cents int64) string {
	return fmt.Sprintf("%s %d,%02d", currency, cents/100, cents%100)
}

var toolRefusals = map[opportunity_usecase.Refusal]string{
	opportunity_usecase.RefusalWonWithoutValue:     "Para marcar o negócio como ganho, informe em \"value\" o valor fechado com o cliente.",
	opportunity_usecase.RefusalNoOpenDeal:          "Esta conversa não tem negócio aberto para marcar como perdido.",
	opportunity_usecase.RefusalLostReasonMissing:   "Informe em \"lost_reason\" o motivo da perda.",
	opportunity_usecase.RefusalTitleMissing:        "Informe em \"title\" um título para o negócio.",
	opportunity_usecase.RefusalStageInvalid:        "Informe em \"stage\" uma etapa aberta do funil configurado.",
	opportunity_usecase.RefusalAmountInvalid:       "O valor deve ser um número positivo com ponto decimal, por exemplo 1500.50.",
	opportunity_usecase.RefusalCurrencyUnsupported: "A moeda configurada na ferramenta não é suportada. Avise um administrador.",
	opportunity_usecase.RefusalPipelineInvalid:     "O funil de negócios configurado para esta ferramenta é inválido. Avise um administrador.",
	opportunity_usecase.RefusalRequiredFields:      "O funil exige campos personalizados que a IA não preenche. Avise um administrador.",
}

func opportunityRefusal(err error) string {
	if refusal, ok := opportunity_usecase.RefusalOf(err); ok {
		return toolRefusals[refusal]
	}
	log.Printf("[ManageOpportunity] unexpected error: %v", err)
	return "Não foi possível registrar o negócio agora. Tente novamente mais tarde."
}

var _ tools.ContextualHandler = (*manageOpportunityTool)(nil)
