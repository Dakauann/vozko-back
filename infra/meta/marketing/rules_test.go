package marketing

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func pauseRule() advertising.AutomatedRule {
	return advertising.AutomatedRule{
		AdAccountID: "77",
		Name:        "Pausar caro",
		Status:      advertising.RuleEnabled,
		Entity:      advertising.RuleAdSet,
		ObjectIDs:   []string{"10", "11"},
		Conditions: []advertising.RuleCondition{
			{Metric: advertising.MetricCostPerResult, Operator: advertising.OperatorGreaterThan, Value: 30.5},
			{Metric: advertising.MetricCTR, Operator: advertising.OperatorLessThan, Value: 0.8},
		},
		Window:    advertising.WindowLast7Days,
		Action:    advertising.RuleAction{Type: advertising.RuleActionPause},
		Frequency: advertising.RuleEvery30Minutes,
	}
}

func TestCreateRuleSerializesSpecsWithMoneyInMinorUnits(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"800"}`))
	id, err := g.CreateRule(context.Background(), "tok", "77", "BRL", pauseRule())
	if err != nil || id != "800" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	call := log.all()[0]
	if call.method != http.MethodPost || call.path != "/act_77/adrules_library" || call.form.Get("name") != "Pausar caro" || call.form.Get("status") != "ENABLED" {
		t.Fatalf("call = %+v", call)
	}
	sameJSON(t, []byte(call.form.Get("evaluation_spec")), `{"evaluation_type":"SCHEDULE","filters":[
		{"field":"entity_type","value":"ADSET","operator":"EQUAL"},
		{"field":"time_preset","value":"LAST_7_DAYS","operator":"EQUAL"},
		{"field":"id","value":["10","11"],"operator":"IN"},
		{"field":"effective_status","value":["ACTIVE"],"operator":"IN"},
		{"field":"cost_per_result","value":3050,"operator":"GREATER_THAN"},
		{"field":"ctr","value":0.8,"operator":"LESS_THAN"}]}`)
	sameJSON(t, []byte(call.form.Get("execution_spec")), `{"execution_type":"PAUSE"}`)
	sameJSON(t, []byte(call.form.Get("schedule_spec")), `{"schedule_type":"SEMI_HOURLY"}`)
}

func TestCreateRuleUsesTheCurrencyOffset(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"800"}`))
	rule := pauseRule()
	rule.ObjectIDs = nil
	rule.Action = advertising.RuleAction{Type: advertising.RuleActionUnpause}
	rule.Conditions = []advertising.RuleCondition{{Metric: advertising.MetricSpent, Operator: advertising.OperatorLessThan, Value: 3000}}
	if _, err := g.CreateRule(context.Background(), "tok", "77", "jpy", rule); err != nil {
		t.Fatal(err)
	}
	sameJSON(t, []byte(log.all()[0].form.Get("evaluation_spec")), `{"evaluation_type":"SCHEDULE","filters":[
		{"field":"entity_type","value":"ADSET","operator":"EQUAL"},
		{"field":"time_preset","value":"LAST_7_DAYS","operator":"EQUAL"},
		{"field":"effective_status","value":["PAUSED"],"operator":"IN"},
		{"field":"spent","value":3000,"operator":"LESS_THAN"}]}`)
}

func TestCreateRuleChangesBudgetByPercent(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"801"}`))
	rule := pauseRule()
	rule.Entity = advertising.RuleCampaign
	rule.ObjectIDs = nil
	rule.Frequency = advertising.RuleDaily
	rule.Conditions = []advertising.RuleCondition{{Metric: advertising.MetricResults, Operator: advertising.OperatorGreaterThan, Value: 20}}
	rule.Action = advertising.RuleAction{Type: advertising.RuleActionChangeBudget, BudgetPercent: -20, BudgetCap: 5000}
	if _, err := g.CreateRule(context.Background(), "tok", "77", "BRL", rule); err != nil {
		t.Fatal(err)
	}
	call := log.all()[0]
	sameJSON(t, []byte(call.form.Get("execution_spec")), `{"execution_type":"CHANGE_CAMPAIGN_BUDGET","execution_options":[
		{"field":"change_spec","value":{"amount":-20,"unit":"PERCENTAGE","limit":5000},"operator":"EQUAL"}]}`)
	sameJSON(t, []byte(call.form.Get("schedule_spec")), `{"schedule_type":"DAILY"}`)
	if strings.Contains(call.form.Get("evaluation_spec"), "effective_status") {
		t.Fatalf("budget rule filtered by status: %s", call.form.Get("evaluation_spec"))
	}

	rule.Entity = advertising.RuleAdSet
	rule.Action.BudgetCap = 0
	if _, err := g.CreateRule(context.Background(), "tok", "77", "BRL", rule); err != nil {
		t.Fatal(err)
	}
	sameJSON(t, []byte(log.all()[1].form.Get("execution_spec")), `{"execution_type":"CHANGE_BUDGET","execution_options":[
		{"field":"change_spec","value":{"amount":-20,"unit":"PERCENTAGE"},"operator":"EQUAL"}]}`)
}

func TestCreateRuleRejectsUnmappableInputBeforeCalling(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"800"}`))
	badCurrency := pauseRule()
	badMetric := pauseRule()
	badMetric.Conditions[0].Metric = "roas"
	badWindow := pauseRule()
	badWindow.Window = "THIS_MONTH"
	badStatus := pauseRule()
	badStatus.Status = "DELETED"
	for name, c := range map[string]struct {
		rule     advertising.AutomatedRule
		currency string
	}{
		"currency": {rule: badCurrency, currency: "R$"},
		"metric":   {rule: badMetric, currency: "BRL"},
		"window":   {rule: badWindow, currency: "BRL"},
		"status":   {rule: badStatus, currency: "BRL"},
	} {
		if _, err := g.CreateRule(context.Background(), "tok", "77", c.currency, c.rule); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if len(log.all()) != 0 {
		t.Fatalf("calls = %+v", log.all())
	}
}

const listedRules = `{"data":[
	{"id":"800","name":"Pausar caro","status":"ENABLED","created_time":"2026-09-30T10:00:00+0000",
	 "evaluation_spec":{"evaluation_type":"SCHEDULE","filters":[
		{"field":"entity_type","value":"ADSET","operator":"EQUAL"},
		{"field":"time_preset","value":"LAST_7_DAYS","operator":"EQUAL"},
		{"field":"adset.id","value":["10",11],"operator":"IN"},
		{"field":"effective_status","value":["ACTIVE"],"operator":"IN"},
		{"field":"cost_per_result","value":3050,"operator":"GREATER_THAN"},
		{"field":"ctr","value":"0.8","operator":"LESS_THAN"},
		{"field":"website_purchase_roas","value":2,"operator":"LESS_THAN"}]},
	 "execution_spec":{"execution_type":"PAUSE"},"schedule_spec":{"schedule_type":"SEMI_HOURLY"}},
	{"id":"801","name":"Escalar","status":"DISABLED",
	 "evaluation_spec":{"evaluation_type":"SCHEDULE","filters":[
		{"field":"entity_type","value":"CAMPAIGN","operator":"EQUAL"},
		{"field":"time_preset","value":"THIS_MONTH","operator":"EQUAL"},
		{"field":"results","value":20,"operator":"GREATER_THAN"}]},
	 "execution_spec":{"execution_type":"CHANGE_CAMPAIGN_BUDGET","execution_options":[
		{"field":"change_spec","value":{"amount":"15","unit":"PERCENTAGE","limit":5000},"operator":"EQUAL"}]},
	 "schedule_spec":{"schedule_type":"CUSTOM"}},
	{"id":"802","name":"Gatilho","status":"ENABLED",
	 "evaluation_spec":{"evaluation_type":"TRIGGER","filters":[{"field":"entity_type","value":"AD","operator":"EQUAL"},{"field":"time_preset","value":"TODAY","operator":"EQUAL"}],
		"trigger":{"type":"STATS_CHANGE","field":"spent","value":10000,"operator":"GREATER_THAN"}},
	 "execution_spec":{"execution_type":"NOTIFICATION"},"schedule_spec":{"schedule_type":"SEMI_HOURLY"}}]}`

func TestListRulesMapsSpecsBackToTheDomain(t *testing.T) {
	g, log := wireGateway(t, func(call wireCall) (int, string) {
		if call.path == "/act_77" {
			return http.StatusOK, `{"currency":"BRL","id":"act_77"}`
		}
		return http.StatusOK, listedRules
	})
	rules, err := g.ListRules(context.Background(), "tok", "act_77")
	if err != nil {
		t.Fatal(err)
	}
	calls := log.all()
	if calls[0].path != "/act_77/adrules_library" || calls[0].query.Get("fields") != ruleFields {
		t.Fatalf("calls = %+v", calls)
	}
	currencyLookups := 0
	for _, call := range calls {
		if call.path == "/act_77" {
			currencyLookups++
		}
	}
	if currencyLookups != 1 || len(rules) != 3 {
		t.Fatalf("currency lookups = %d rules = %+v", currencyLookups, rules)
	}

	pause := rules[0]
	wantConditions := []advertising.RuleCondition{
		{Metric: advertising.MetricCostPerResult, Operator: advertising.OperatorGreaterThan, Value: 30.5},
		{Metric: advertising.MetricCTR, Operator: advertising.OperatorLessThan, Value: 0.8},
		{Metric: "website_purchase_roas", Operator: advertising.OperatorLessThan, Value: 2},
	}
	if pause.MetaID != "800" || pause.AdAccountID != "77" || pause.Status != advertising.RuleEnabled || pause.Entity != advertising.RuleAdSet ||
		pause.Window != advertising.WindowLast7Days || pause.Action.Type != advertising.RuleActionPause || pause.Frequency != advertising.RuleEvery30Minutes ||
		!slices.Equal(pause.ObjectIDs, []string{"10", "11"}) || !slices.Equal(pause.Conditions, wantConditions) ||
		pause.CreatedTime == nil || !pause.CreatedTime.Equal(time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("pause = %+v", pause)
	}

	budget := rules[1]
	if budget.Entity != advertising.RuleCampaign || budget.Window != "THIS_MONTH" || budget.Frequency != "CUSTOM" || budget.Status != advertising.RuleDisabled ||
		budget.Action != (advertising.RuleAction{Type: advertising.RuleActionChangeBudget, BudgetPercent: 15, BudgetCap: 5000}) ||
		!slices.Equal(budget.Conditions, []advertising.RuleCondition{{Metric: advertising.MetricResults, Operator: advertising.OperatorGreaterThan, Value: 20}}) {
		t.Fatalf("budget = %+v", budget)
	}

	trigger := rules[2]
	if trigger.Entity != advertising.RuleAd || trigger.Action.Type != "NOTIFICATION" ||
		!slices.Equal(trigger.Conditions, []advertising.RuleCondition{{Metric: advertising.MetricSpent, Operator: advertising.OperatorGreaterThan, Value: 100}}) {
		t.Fatalf("trigger = %+v", trigger)
	}
}

func TestListRulesSkipsTheCurrencyLookupWithoutMoneyConditions(t *testing.T) {
	g, log := wireGateway(t, reply(`{"data":[{"id":"801","name":"x","status":"ENABLED","evaluation_spec":{"filters":[{"field":"entity_type","value":"AD","operator":"EQUAL"},{"field":"reach","value":100,"operator":"GREATER_THAN"}]},"execution_spec":{"execution_type":"PAUSE"}}]}`))
	if _, err := g.ListRules(context.Background(), "tok", "77"); err != nil {
		t.Fatal(err)
	}
	if len(log.all()) != 1 {
		t.Fatalf("calls = %+v", log.all())
	}
}

func TestListRulesRejectsUnparseableRules(t *testing.T) {
	cases := map[string]string{
		"non numeric value": `{"field":"objective","value":["OUTCOME_LEADS"],"operator":"IN"}`,
		"fractional money":  `{"field":"spent","value":10.5,"operator":"GREATER_THAN"}`,
	}
	for name, filter := range cases {
		g, _ := wireGateway(t, func(call wireCall) (int, string) {
			if call.path == "/act_77" {
				return http.StatusOK, `{"currency":"BRL"}`
			}
			return http.StatusOK, `{"data":[{"id":"801","evaluation_spec":{"filters":[` + filter + `]},"execution_spec":{"execution_type":"PAUSE"}}]}`
		})
		if _, err := g.ListRules(context.Background(), "tok", "77"); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	executions := map[string]string{
		"currency unit":     `{"execution_type":"CHANGE_BUDGET","execution_options":[{"field":"change_spec","value":{"amount":500,"unit":"ACCOUNT_CURRENCY"},"operator":"EQUAL"}]}`,
		"missing change":    `{"execution_type":"CHANGE_BUDGET"}`,
		"fractional amount": `{"execution_type":"CHANGE_BUDGET","execution_options":[{"field":"change_spec","value":{"amount":2.5,"unit":"PERCENTAGE"},"operator":"EQUAL"}]}`,
	}
	for name, execution := range executions {
		g, _ := wireGateway(t, reply(`{"data":[{"id":"801","evaluation_spec":{"filters":[]},"execution_spec":`+execution+`}]}`))
		if _, err := g.ListRules(context.Background(), "tok", "77"); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestSetRuleStatusAndDeleteRuleRequireSuccess(t *testing.T) {
	g, log := wireGateway(t, reply(`{"success":true}`))
	if err := g.SetRuleStatus(context.Background(), "tok", "800", advertising.RuleDisabled); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteRule(context.Background(), "tok", "800"); err != nil {
		t.Fatal(err)
	}
	calls := log.all()
	if calls[0].method != http.MethodPost || calls[0].path != "/800" || calls[0].form.Get("status") != "DISABLED" || calls[1].method != http.MethodDelete || calls[1].path != "/800" {
		t.Fatalf("calls = %+v", calls)
	}
	if err := g.SetRuleStatus(context.Background(), "tok", "800", "PAUSED"); err == nil {
		t.Fatal("expected an error for an unknown status")
	}
	refused, _ := wireGateway(t, reply(`{}`))
	if err := refused.DeleteRule(context.Background(), "tok", "800"); err == nil {
		t.Fatal("expected an error when meta does not acknowledge")
	}
}

func TestRuleHistoryMapsRunsAndObjects(t *testing.T) {
	g, log := wireGateway(t, reply(`{"data":[{"timestamp":"2026-10-01T09:30:00+0000","is_manual":false,"execution_spec":{"execution_type":"PAUSE"},
		"results":[{"object_id":"10","object_type":"ADSET","actions":[]},{"object_id":11,"object_type":"ADSET"}]}]}`))
	runs, err := g.RuleHistory(context.Background(), "tok", "800")
	if err != nil {
		t.Fatal(err)
	}
	if call := log.all()[0]; call.path != "/800/history" {
		t.Fatalf("call = %+v", call)
	}
	if len(runs) != 1 || runs[0].Result != "PAUSE" || !slices.Equal(runs[0].Objects, []string{"10", "11"}) || !runs[0].At.Equal(time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("runs = %+v", runs)
	}
	missing, _ := wireGateway(t, reply(`{"data":[{"execution_spec":{"execution_type":"PAUSE"}}]}`))
	if _, err := missing.RuleHistory(context.Background(), "tok", "800"); err == nil {
		t.Fatal("expected an error without a timestamp")
	}
}
