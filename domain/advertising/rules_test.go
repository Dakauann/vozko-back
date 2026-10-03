package advertising

import "testing"

func validRule() AutomatedRule {
	r := AutomatedRule{
		AdAccountID: "a", Name: "Pausar caro", Entity: RuleAdSet,
		Conditions: []RuleCondition{{Metric: MetricCostPerResult, Operator: OperatorGreaterThan, Value: 30}},
		Action:     RuleAction{Type: RuleActionPause},
	}
	r.Normalize()
	return r
}

func TestRuleDefaultsAndValidRule(t *testing.T) {
	r := validRule()
	if r.Status != RuleEnabled || r.Frequency != RuleEvery30Minutes || r.Window != WindowToday {
		t.Fatalf("defaults %+v", r)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetRulesNeedAPercentAndNeverTargetAds(t *testing.T) {
	r := validRule()
	r.Entity = RuleAd
	r.Action = RuleAction{Type: RuleActionChangeBudget}
	requireIssues(t, r.Validate(), FieldIssue{"action.type", "not_for_ads"}, FieldIssue{"action.budgetPercent", "invalid"})
}

func TestRuleNeedsAValidCondition(t *testing.T) {
	r := validRule()
	r.Conditions = []RuleCondition{{Metric: "mood", Operator: "ABOUT", Value: -1}}
	requireIssues(t, r.Validate(),
		FieldIssue{"conditions[0].metric", "invalid"},
		FieldIssue{"conditions[0].operator", "invalid"},
		FieldIssue{"conditions[0].value", "invalid"},
	)
	if !MetricSpent.Money() || MetricCTR.Money() {
		t.Fatal("money metrics wrong")
	}
}
