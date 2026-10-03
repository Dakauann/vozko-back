package copilottools

import (
	"context"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

type AdRules interface {
	List(ctx context.Context, workspaceID, accountID string) ([]advertising.AutomatedRule, error)
	Check(ctx context.Context, workspaceID string, rule advertising.AutomatedRule) (*adsuc.RuleCheck, error)
	Create(ctx context.Context, workspaceID string, rule advertising.AutomatedRule) (string, error)
	SetEnabled(ctx context.Context, workspaceID, accountID, ruleID string, enabled bool) error
	Delete(ctx context.Context, workspaceID, accountID, ruleID string) error
	History(ctx context.Context, workspaceID, accountID, ruleID string) ([]advertising.RuleRun, error)
}

const maxRuleRunsShown = 20

var ruleEntities = map[advertising.Level]advertising.RuleEntity{
	advertising.LevelCampaign: advertising.RuleCampaign,
	advertising.LevelAdSet:    advertising.RuleAdSet,
	advertising.LevelAd:       advertising.RuleAd,
}

var ruleMetricNames = map[advertising.RuleMetric]string{
	advertising.MetricSpent:         "gasto",
	advertising.MetricResults:       "resultados",
	advertising.MetricCostPerResult: "custo por resultado",
	advertising.MetricImpressions:   "impressões",
	advertising.MetricReach:         "alcance",
	advertising.MetricFrequency:     "frequência",
	advertising.MetricCPC:           "custo por clique",
	advertising.MetricCPM:           "custo por mil impressões",
	advertising.MetricCTR:           "taxa de cliques (%)",
}

var ruleWindowNames = map[advertising.RuleWindow]string{
	advertising.WindowToday:      "hoje",
	advertising.WindowYesterday:  "ontem",
	advertising.WindowLast3Days:  "últimos 3 dias",
	advertising.WindowLast7Days:  "últimos 7 dias",
	advertising.WindowLast14Days: "últimos 14 dias",
	advertising.WindowLast30Days: "últimos 30 dias",
	advertising.WindowLifetime:   "todo o período",
}

var ruleFrequencyNames = map[advertising.RuleFrequency]string{
	advertising.RuleEvery30Minutes: "a cada 30 minutos",
	advertising.RuleHourly:         "a cada hora",
	advertising.RuleDaily:          "uma vez por dia",
}

func objectNames(objects []*advertising.Object) string {
	names := make([]string, 0, len(objects))
	for _, o := range objects {
		names = append(names, o.Name)
	}
	return strings.Join(names, ", ")
}

func conditionText(currency string, c advertising.RuleCondition) string {
	comparison := "maior que"
	if c.Operator == advertising.OperatorLessThan {
		comparison = "menor que"
	}
	value := strconv.FormatFloat(c.Value, 'f', -1, 64)
	if c.Metric.Money() {
		value = amountText(currency, c.Value, value)
	}
	return ruleMetricNames[c.Metric] + " " + comparison + " " + value
}

func amountText(currency string, amount float64, fallback string) string {
	minor, err := advertising.AmountToMinor(currency, amount)
	if err != nil {
		return fallback
	}
	micros, err := advertising.MinorToMicros(currency, minor)
	if err != nil {
		return fallback
	}
	return moneyText(currency, micros)
}

func ruleActionText(a advertising.RuleAction) string {
	switch a.Type {
	case advertising.RuleActionPause:
		return "desligar"
	case advertising.RuleActionUnpause:
		return "ligar"
	case advertising.RuleActionChangeBudget:
		if a.BudgetPercent < 0 {
			return "diminuir o orçamento em " + strconv.Itoa(-a.BudgetPercent) + "%"
		}
		return "aumentar o orçamento em " + strconv.Itoa(a.BudgetPercent) + "%"
	}
	return string(a.Type)
}

func ruleRow(currency string, r advertising.AutomatedRule) map[string]interface{} {
	conditions := make([]string, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		conditions = append(conditions, conditionText(currency, c))
	}
	return map[string]interface{}{
		"rule_id": r.MetaID, "name": r.Name, "status": string(r.Status), "level": string(r.Entity),
		"objects": len(r.ObjectIDs), "conditions": conditions, "window": ruleWindowNames[r.Window],
		"action": ruleActionText(r.Action), "frequency": ruleFrequencyNames[r.Frequency],
	}
}

type listAdRulesTool struct{ adGrowth }

func (t *listAdRulesTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdRulesTool) Definition() tools.Definition {
	return definition("list_ad_rules",
		"Lista as regras automáticas da conta na Meta: condição, ação, com que frequência a Meta confere e se estão ligadas. Traz o rule_id de cada regra.",
		adAccountArgs{})
}

func (t *listAdRulesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return growthFailure("list_ad_rules", "", err)
	}
	rules, err := t.deps.Rules.List(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return growthFailure("list_ad_rules", account.ID, err)
	}
	out := make([]map[string]interface{}, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleRow(account.Currency, r))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"rules": out}}
}

type createAdRuleArgs struct {
	AdAccountID   string   `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	Name          string   `json:"name" req:"true" desc:"nome da regra"`
	Level         string   `json:"level" req:"true" enum:"campaign,adset,ad" desc:"em que nível a regra age: campaign, adset ou ad"`
	ObjectIDs     []string `json:"object_ids" desc:"meta_id de ads_results, do mesmo level; vazio vale para todos os itens ativos desse nível na conta"`
	Metric        string   `json:"metric" req:"true" enum:"spent,results,cost_per_result,impressions,reach,frequency,cpc,cpm,ctr" desc:"métrica conferida: spent (gasto), results, cost_per_result, impressions, reach, frequency, cpc, cpm, ctr"`
	Operator      string   `json:"operator" req:"true" enum:"GREATER_THAN,LESS_THAN" desc:"GREATER_THAN (maior que) ou LESS_THAN (menor que)"`
	Value         float64  `json:"value" desc:"limite; valores em dinheiro na moeda da conta, 20 significa 20 reais em uma conta BRL"`
	Window        string   `json:"window" enum:"TODAY,YESTERDAY,LAST_3_DAYS,LAST_7_DAYS,LAST_14_DAYS,LAST_30_DAYS,LIFETIME" desc:"período das métricas (padrão TODAY)"`
	Action        string   `json:"action" req:"true" enum:"PAUSE,UNPAUSE,CHANGE_BUDGET" desc:"PAUSE desliga, UNPAUSE liga, CHANGE_BUDGET muda o orçamento (não vale para level ad)"`
	BudgetPercent int      `json:"budget_percent" desc:"para CHANGE_BUDGET: de -100 a 100, ex.: 20 aumenta 20%, -20 diminui 20%"`
	Frequency     string   `json:"frequency" enum:"SEMI_HOURLY,HOURLY,DAILY" desc:"com que frequência a Meta confere (padrão SEMI_HOURLY, a cada 30 minutos)"`
}

type rulePlan struct {
	account *advertising.AdAccount
	objects []*advertising.Object
	rule    advertising.AutomatedRule
}

type createAdRuleTool struct{ adGrowth }

func (t *createAdRuleTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createAdRuleTool) Definition() tools.Definition {
	return definition("create_ad_rule",
		"Cria uma regra automática na Meta: quando a métrica passar do limite no período, a Meta desliga, liga ou muda o orçamento dos itens sozinha. "+
			"A regra já nasce ligada. Só depois da aprovação do usuário.",
		createAdRuleArgs{})
}

func (t *createAdRuleTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*rulePlan, error) {
	a, err := validateArgs[createAdRuleArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	ids, err := metaIDs(a.ObjectIDs)
	if err != nil {
		return nil, err
	}
	checked, err := t.deps.Rules.Check(ctx, cc.WorkspaceID, advertising.AutomatedRule{
		AdAccountID: account.ID, Name: a.Name, Entity: ruleEntities[advertising.Level(a.Level)], ObjectIDs: ids,
		Conditions: []advertising.RuleCondition{{Metric: advertising.RuleMetric(a.Metric), Operator: advertising.RuleOperator(a.Operator), Value: a.Value}},
		Window:     advertising.RuleWindow(a.Window), Frequency: advertising.RuleFrequency(a.Frequency),
		Action: advertising.RuleAction{Type: advertising.RuleActionType(a.Action), BudgetPercent: a.BudgetPercent},
	})
	if err != nil {
		return nil, err
	}
	return &rulePlan{account: account, objects: checked.Objects, rule: checked.Rule}, nil
}

func (t *createAdRuleTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_ad_rule", err)
}

func (t *createAdRuleTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "rule", Value: "regra com campos a corrigir"}}
	}
	applies := objectNames(p.objects)
	if applies == "" {
		applies = "todos os itens ativos desse nível na conta"
	}
	return []copilot.Field{
		{Key: "rule", Value: p.rule.Name},
		{Key: "when", Value: conditionText(p.account.Currency, p.rule.Conditions[0]) + " (" + ruleWindowNames[p.rule.Window] + ")"},
		{Key: "action", Value: ruleActionText(p.rule.Action)},
		{Key: "applies", Value: applies},
		{Key: "checks", Value: ruleFrequencyNames[p.rule.Frequency]},
	}
}

func (t *createAdRuleTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_ad_rule", "", err)
	}
	id, err := t.deps.Rules.Create(ctx, cc.WorkspaceID, p.rule)
	if err != nil {
		return growthFailure("create_ad_rule", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"rule_id": id, "name": p.rule.Name}}
}

type adRuleArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	RuleID      string `json:"rule_id" req:"true" desc:"rule_id exato de list_ad_rules"`
}

type ruleTarget struct {
	account *advertising.AdAccount
	rule    advertising.AutomatedRule
}

func (g adGrowth) existingRule(ctx context.Context, cc copilot.Context, accountID, ruleID string) (*ruleTarget, error) {
	account, err := g.ads.account(ctx, cc, accountID)
	if err != nil {
		return nil, err
	}
	rules, err := g.deps.Rules.List(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(ruleID)
	for _, r := range rules {
		if r.MetaID == id {
			return &ruleTarget{account: account, rule: r}, nil
		}
	}
	return nil, advertising.ErrRuleNotFound
}

type setAdRuleStatusArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	RuleID      string `json:"rule_id" req:"true" desc:"rule_id exato de list_ad_rules"`
	Status      string `json:"status" req:"true" enum:"ENABLED,DISABLED" desc:"ENABLED liga a regra, DISABLED desliga"`
}

type setAdRuleStatusTool struct{ adGrowth }

func (t *setAdRuleStatusTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *setAdRuleStatusTool) Definition() tools.Definition {
	return definition("set_ad_rule_status",
		"Liga ou desliga uma regra automática da Meta, sem apagar. Só depois da aprovação do usuário.",
		setAdRuleStatusArgs{})
}

func (t *setAdRuleStatusTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*ruleTarget, bool, error) {
	a, err := validateArgs[setAdRuleStatusArgs](nil, cc, args)
	if err != nil {
		return nil, false, err
	}
	target, err := t.existingRule(ctx, cc, a.AdAccountID, a.RuleID)
	return target, advertising.RuleStatus(a.Status) == advertising.RuleEnabled, err
}

func (t *setAdRuleStatusTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, _, err := t.plan(ctx, cc, args)
	return adsValidation("set_ad_rule_status", err)
}

func (t *setAdRuleStatusTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	target, enabled, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "rule", Value: "regra desconhecida"}}
	}
	change := "desligar"
	if enabled {
		change = "ligar"
	}
	return []copilot.Field{{Key: "rule", Value: target.rule.Name}, {Key: "change", Value: change}}
}

func (t *setAdRuleStatusTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	target, enabled, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("set_ad_rule_status", "", err)
	}
	if err := t.deps.Rules.SetEnabled(ctx, cc.WorkspaceID, target.account.ID, target.rule.MetaID, enabled); err != nil {
		return growthFailure("set_ad_rule_status", target.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"rule_id": target.rule.MetaID, "enabled": enabled}}
}

type deleteAdRuleTool struct{ adGrowth }

func (t *deleteAdRuleTool) Meta() copilot.Meta { return adsMeta(workspace.ActionDelete, true) }

func (t *deleteAdRuleTool) Definition() tools.Definition {
	return definition("delete_ad_rule",
		"Apaga uma regra automática da Meta. Para só pausar, use set_ad_rule_status. Só depois da aprovação do usuário.",
		adRuleArgs{})
}

func (t *deleteAdRuleTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*ruleTarget, error) {
	a, err := validateArgs[adRuleArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	return t.existingRule(ctx, cc, a.AdAccountID, a.RuleID)
}

func (t *deleteAdRuleTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("delete_ad_rule", err)
}

func (t *deleteAdRuleTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	target, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "rule", Value: "regra desconhecida"}}
	}
	return []copilot.Field{{Key: "rule", Value: target.rule.Name}, {Key: "action", Value: ruleActionText(target.rule.Action)}}
}

func (t *deleteAdRuleTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	target, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("delete_ad_rule", "", err)
	}
	if err := t.deps.Rules.Delete(ctx, cc.WorkspaceID, target.account.ID, target.rule.MetaID); err != nil {
		return growthFailure("delete_ad_rule", target.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": target.rule.Name}}
}

type adRuleHistoryTool struct{ adGrowth }

func (t *adRuleHistoryTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *adRuleHistoryTool) Definition() tools.Definition {
	return definition("ad_rule_history",
		"Mostra as últimas vezes que uma regra automática rodou na Meta, o resultado e quantos itens ela mudou.",
		adRuleArgs{})
}

func (t *adRuleHistoryTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adRuleArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return growthFailure("ad_rule_history", "", err)
	}
	runs, err := t.deps.Rules.History(ctx, cc.WorkspaceID, account.ID, strings.TrimSpace(a.RuleID))
	if err != nil {
		return growthFailure("ad_rule_history", account.ID, err)
	}
	if len(runs) > maxRuleRunsShown {
		runs = runs[:maxRuleRunsShown]
	}
	loc, err := account.Location()
	if err != nil {
		return growthFailure("ad_rule_history", account.ID, err)
	}
	out := make([]map[string]interface{}, 0, len(runs))
	for _, r := range runs {
		out = append(out, map[string]interface{}{"at": r.At.In(loc).Format("2006-01-02 15:04"), "result": r.Result, "objects_changed": len(r.Objects)})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"runs": out}}
}
