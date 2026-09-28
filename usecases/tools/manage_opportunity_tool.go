package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/opportunity"
	"vozko/domain/stage"
	"vozko/domain/tools"
	opportunity_usecase "vozko/usecases/opportunity"
)

const (
	ManageOpportunityToolName     = "manage_opportunity"
	AutoManageOpportunityToolName = "auto_manage_opportunity"
	OpportunityPipelinesSource    = "opportunity_pipelines"
	readOpportunityAction         = "get"
)

type OpportunityManager interface {
	PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error)
	DealsForEntry(workspaceID, pipelineID, entryID, entryType string) (opportunity.EntryDeals, error)
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

type ConversationOwners interface {
	ConversationOwner(workspaceID, entryID, entryType string) (string, error)
}

type dealIdentity struct {
	author string
	owner  string
}

type dealToolVariant struct {
	define   func() tools.Definition
	identify func(ctx context.Context, config map[string]interface{}) (dealIdentity, string)
	settings func(config map[string]interface{}) opportunitySettings
}

type manageOpportunityTool struct {
	deals   OpportunityManager
	variant dealToolVariant
}

func NewManageOpportunityTool(deals OpportunityManager) tools.Handler {
	if deals == nil {
		return nil
	}
	return &manageOpportunityTool{deals: deals, variant: dealToolVariant{
		define:   agentDealDefinition,
		identify: agentIdentity,
		settings: opportunitySettingsFrom,
	}}
}

func NewAutoManageOpportunityTool(deals OpportunityManager, owners ConversationOwners) tools.Handler {
	if deals == nil || owners == nil {
		return nil
	}
	return &manageOpportunityTool{deals: deals, variant: dealToolVariant{
		define:   autoDealDefinition,
		identify: conversationIdentity(owners),
		settings: autoDealSettings,
	}}
}

func agentIdentity(ctx context.Context, config map[string]interface{}) (dealIdentity, string) {
	actorID := agentActor(ctx, config)
	if actorID == "" {
		return dealIdentity{}, "Esta ferramenta só pode ser usada por um agente de IA identificado."
	}
	return dealIdentity{author: actorID}, ""
}

func conversationIdentity(owners ConversationOwners) func(context.Context, map[string]interface{}) (dealIdentity, string) {
	return func(_ context.Context, config map[string]interface{}) (dealIdentity, string) {
		owner, err := owners.ConversationOwner(configString(config, "__workspace_id"), configString(config, "__entry_id"), configString(config, "__entry_type"))
		if err != nil {
			log.Printf("[AutoManageOpportunity] owner lookup failed: %v", err)
			return dealIdentity{}, "Não foi possível identificar o responsável pela conversa agora. Nenhuma oportunidade foi alterada."
		}
		if owner == "" {
			if agentID := configString(config, "__agent_id"); agentID != "" {
				owner = actor.FormatAI(agentID)
			}
		}
		if owner == "" {
			return dealIdentity{}, "A conversa não tem responsável nem agente de IA. Nenhuma oportunidade foi registrada."
		}
		return dealIdentity{author: actor.SystemID, owner: owner}, ""
	}
}

func autoDealSettings(config map[string]interface{}) opportunitySettings {
	settings := opportunitySettings{pipelineID: configString(config, "pipeline_id"), allowed: map[opportunity_usecase.EntryAction]bool{}}
	for _, action := range opportunity_usecase.EntryActions() {
		settings.allowed[action] = true
	}
	return settings
}

func autoDealDefinition() tools.Definition {
	def := agentDealDefinition()
	def.Name = AutoManageOpportunityToolName
	def.DisplayName = "Oportunidades automáticas"
	def.DisplayDescription = "Registra e atualiza as oportunidades da conversa a partir da análise automática. O responsável pela conversa fica como dono da oportunidade."
	def.Visibility = []tools.ToolVisibility{tools.VisibilityAnalysis}
	def.Category = tools.CategoryAgentUtility
	def.ConfigSchema = nil
	def.RequiredConfig = nil
	def.RequiresConfig = false
	return def
}

func (t *manageOpportunityTool) Definition() tools.Definition {
	return t.variant.define()
}

func agentDealDefinition() tools.Definition {
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
		DisplayName:        "Gerenciar Oportunidade da Conversa",
		DisplayDescription: "Cria e atualiza as oportunidades desta conversa no funil de oportunidades: valor, etapa, ganho ou perda. A IA fica como responsável pelas oportunidades que criar.",
		Description: `Gerencia as oportunidades de venda desta conversa neste funil. Uma conversa pode tratar mais de um contrato, cada um com sua oportunidade.

AÇÕES:
- get: lista as oportunidades da conversa com o id de cada uma. Use antes de alterar quando não souber qual oportunidade é.
- create: abre a oportunidade quando o cliente demonstra intenção real de compra ou atualiza a oportunidade aberta. Informe "title" e, se souber, "value".
- create_new: abre OUTRA oportunidade, para um contrato diferente dos que já existem. Não use "opportunity_id".
- update_value: atualiza o valor negociado em "value".
- move: move a oportunidade para a etapa "stage".
- win: marca como ganho quando o cliente CONFIRMA a compra. Exige "value" com o valor fechado.
- lose: marca como perdido quando o cliente desiste. Exige "lost_reason".

"opportunity_id" escolhe a oportunidade. Sem ele, a ação vale para a única oportunidade aberta da conversa; com mais de uma aberta, a ferramenta recusa e devolve a lista para você escolher.
"value" é um número na moeda da oportunidade, com ponto decimal (ex.: 1500.50). NUNCA invente valores: use apenas o que foi combinado na conversa.`,
		Parameters: map[string]tools.Parameter{
			"action": {
				Type:        "string",
				Description: "Ação a executar.",
				Enum:        append([]string{readOpportunityAction}, entryActionNames(opportunity_usecase.EntryActions())...),
			},
			"opportunity_id": {
				Type:        "string",
				Description: "Id exato de uma oportunidade retornada por get. Obrigatório quando a conversa tem mais de uma oportunidade aberta.",
			},
			"title": {
				Type:        "string",
				Description: "Título curto da oportunidade, como o produto ou plano negociado.",
			},
			"value": {
				Type:        "number",
				Description: "Valor da oportunidade na unidade da moeda, com ponto decimal (ex.: 1500.50).",
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
				Description:        "Funil de oportunidades onde a IA cria e move a oportunidade da conversa.",
				DisplayName:        "Funil de oportunidades",
				DisplayDescription: "Funil de oportunidades onde a IA registra a oportunidade desta conversa.",
				OptionsSource:      OpportunityPipelinesSource,
				Required:           true,
			},
			"allowed_actions": {
				Type:               "array",
				Description:        "Ações que a IA pode executar na oportunidade.",
				DisplayName:        "Ações permitidas",
				DisplayDescription: "O que a IA pode fazer com as oportunidades. Abrir outra oportunidade e marcar como ganho ficam desligados até você permitir.",
				Default:            defaults,
				Options:            actionOptions,
			},
			"currency": {
				Type:               "string",
				Description:        "Moeda dos valores informados pela IA.",
				DisplayName:        "Moeda",
				DisplayDescription: "Moeda das oportunidades criadas pela IA.",
				Options:            currencyOptions(),
			},
		},
		RequiredConfig: []string{"pipeline_id"},
		RequiresConfig: true,
	}
}

var entryActionLabels = map[opportunity_usecase.EntryAction]string{
	opportunity_usecase.EntryCreate:      "Criar oportunidade",
	opportunity_usecase.EntryCreateNew:   "Abrir outra oportunidade",
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
	settings := t.variant.settings(ctx.Config)

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
	identity, refusal := t.variant.identify(ctx, config)
	if refusal != "" {
		return refuse(refusal), nil
	}
	settings := t.variant.settings(config)
	if settings.pipelineID == "" {
		return refuse("A ferramenta está sem funil de oportunidades configurado. Avise um administrador."), nil
	}

	name, _ := params["action"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == readOpportunityAction {
		return t.listDeals(workspaceID, entryID, entryType, settings.pipelineID), nil
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
		EntryID:       entryID,
		EntryType:     entryType,
		LeadID:        configString(config, "__lead_id"),
		PipelineID:    settings.pipelineID,
		Actor:         identity.author,
		Owner:         identity.owner,
		Action:        action,
		OpportunityID: paramString(params, "opportunity_id"),
		Title:         paramString(params, "title"),
		Currency:      settings.currency,
		LostReasonID:  paramString(params, "lost_reason"),
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
			return refuse(fmt.Sprintf("Etapa %q não encontrada entre as etapas abertas do funil: %s. Para encerrar a oportunidade use win ou lose.",
				target, strings.Join(openStageNames(stages), ", "))), nil
		}
		cmd.StageID = stageID
	}

	result, err := t.deals.ManageForEntry(workspaceID, cmd)
	if errors.Is(err, opportunity.ErrAmbiguousDeal) {
		listing := t.listDeals(workspaceID, entryID, entryType, settings.pipelineID)
		return refuse(fmt.Sprintf("%s\n%v", opportunityRefusal(err), listing.Result)), nil
	}
	if err != nil {
		return refuse(opportunityRefusal(err)), nil
	}
	summary := describeDeal(result.Opportunity, stages)
	verb := "atualizada"
	if result.Created {
		verb = "criada"
	}
	return tools.ExecutionResult{
		Result:            fmt.Sprintf("Oportunidade %s. %s", verb, summary),
		ContextUpdateText: summary,
	}, nil
}

func (t *manageOpportunityTool) listDeals(workspaceID, entryID, entryType, pipelineID string) tools.ExecutionResult {
	described, err := DescribeEntryDeals(t.deals, workspaceID, pipelineID, entryID, entryType)
	if err != nil {
		return refuse(opportunityRefusal(err))
	}
	return tools.ExecutionResult{Result: described}
}

func DescribeEntryDeals(deals OpportunityManager, workspaceID, pipelineID, entryID, entryType string) (string, error) {
	list, err := deals.DealsForEntry(workspaceID, pipelineID, entryID, entryType)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "Esta conversa ainda não tem oportunidade neste funil.", nil
	}
	stages, err := deals.PipelineStages(workspaceID, pipelineID)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(list))
	for _, deal := range list {
		lines = append(lines, fmt.Sprintf("- id %s: %s", deal.ID, describeDeal(deal, stages)))
	}
	return "Oportunidades desta conversa:\n" + strings.Join(lines, "\n"), nil
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
	return fmt.Sprintf("Oportunidade %q na etapa %q, valor %s, status %s.",
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
	opportunity_usecase.RefusalWonWithoutValue:     "Para marcar a oportunidade como ganha, informe em \"value\" o valor fechado com o cliente.",
	opportunity_usecase.RefusalNoOpenDeal:          "Esta conversa não tem oportunidade aberta neste funil. Use create para abrir uma.",
	opportunity_usecase.RefusalLostReasonMissing:   "Informe em \"lost_reason\" o motivo da perda.",
	opportunity_usecase.RefusalTitleMissing:        "Informe em \"title\" um título para a oportunidade.",
	opportunity_usecase.RefusalStageInvalid:        "Informe em \"stage\" uma etapa aberta do funil configurado.",
	opportunity_usecase.RefusalAmountInvalid:       "O valor deve ser um número positivo com ponto decimal, por exemplo 1500.50.",
	opportunity_usecase.RefusalCurrencyUnsupported: "A moeda configurada na ferramenta não é suportada. Avise um administrador.",
	opportunity_usecase.RefusalPipelineInvalid:     "O funil de oportunidades configurado para esta ferramenta é inválido. Avise um administrador.",
	opportunity_usecase.RefusalRequiredFields:      "O funil exige campos personalizados que a IA não preenche. Avise um administrador.",
	opportunity_usecase.RefusalAmbiguousDeal:       "Esta conversa tem mais de uma oportunidade aberta. Repita a ação informando em \"opportunity_id\" o id da oportunidade certa:",
	opportunity_usecase.RefusalDealNotLinked:       "Esse \"opportunity_id\" não é de uma oportunidade desta conversa. Use get para ver os ids.",
	opportunity_usecase.RefusalDealClosed:          "Essa oportunidade já foi encerrada e não pode mais ser alterada pela IA.",
	opportunity_usecase.RefusalDealIDOnNewDeal:     "create_new abre uma oportunidade nova: não informe \"opportunity_id\".",
	opportunity_usecase.RefusalDealChanged:         "A oportunidade acabou de ser alterada por outra pessoa ou automação. Use get para ver o estado atual antes de tentar de novo.",
}

func opportunityRefusal(err error) string {
	if refusal, ok := opportunity_usecase.RefusalOf(err); ok {
		return toolRefusals[refusal]
	}
	log.Printf("[ManageOpportunity] unexpected error: %v", err)
	return "Não foi possível registrar a oportunidade agora. Tente novamente mais tarde."
}

var _ tools.ContextualHandler = (*manageOpportunityTool)(nil)
