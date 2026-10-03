package copilottools

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	adsuc "vozko/usecases/advertising"
)

type stubRules struct {
	objects map[string]*advertising.Object
	rules   []advertising.AutomatedRule
	created *advertising.AutomatedRule
	enabled []bool
	deleted []string
}

func (r *stubRules) List(context.Context, string, string) ([]advertising.AutomatedRule, error) {
	return r.rules, nil
}

var stubRuleLevels = map[advertising.RuleEntity]advertising.Level{advertising.RuleCampaign: advertising.LevelCampaign, advertising.RuleAdSet: advertising.LevelAdSet, advertising.RuleAd: advertising.LevelAd}

func (r *stubRules) Check(_ context.Context, _ string, rule advertising.AutomatedRule) (*adsuc.RuleCheck, error) {
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	objects := []*advertising.Object{}
	for _, id := range rule.ObjectIDs {
		o, ok := r.objects[id]
		if !ok || o.AdAccountID != rule.AdAccountID || o.Level != stubRuleLevels[rule.Entity] {
			return nil, advertising.FieldError("objectIds", "not_available")
		}
		objects = append(objects, o)
	}
	return &adsuc.RuleCheck{Rule: rule, Objects: objects}, nil
}
func (r *stubRules) Create(_ context.Context, _ string, rule advertising.AutomatedRule) (string, error) {
	r.created = &rule
	return "778", nil
}
func (r *stubRules) SetEnabled(_ context.Context, _, _, _ string, enabled bool) error {
	r.enabled = append(r.enabled, enabled)
	return nil
}
func (r *stubRules) Delete(_ context.Context, _, _, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *stubRules) History(_ context.Context, _, _, id string) ([]advertising.RuleRun, error) {
	if id != "777" {
		return nil, advertising.ErrRuleNotFound
	}
	return []advertising.RuleRun{{At: adTestClock, Result: "SUCCESS", Objects: []string{"120300", "120301"}}}, nil
}

func ruleArgsMap() map[string]interface{} {
	return map[string]interface{}{
		"ad_account_id": adAccountUUID, "name": "Pausar caro", "level": "adset", "object_ids": []interface{}{"120300"},
		"metric": "cost_per_result", "operator": "GREATER_THAN", "value": 20.0, "action": "PAUSE",
	}
}

func TestRuleIsCheckedByTheDomainBeforeApproval(t *testing.T) {
	tools, _ := growthTools()
	tool := tools["create_ad_rule"]
	args := ruleArgsMap()
	args["level"], args["action"], args["budget_percent"], args["object_ids"] = "ad", "CHANGE_BUDGET", 20, []interface{}{}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("budget on ad err %v", err)
	}
	args = ruleArgsMap()
	args["action"] = "CHANGE_BUDGET"
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("no percent err %v", err)
	}
}

func TestRuleActsOnlyOnItemsOfTheAccountAtItsLevel(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_ad_rule"]
	args := ruleArgsMap()
	args["object_ids"] = []interface{}{"120200"}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("campaign as adset err %v", err)
	}
	fields := growthDescribe(tool, ruleArgsMap())
	if fields["when"] != "custo por resultado maior que BRL 20,00 (hoje)" || fields["action"] != "desligar" || fields["applies"] != "Conjunto A" {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, ruleArgsMap())
	r := s.rules.created
	if result.Status != copilot.StatusOK || r.Entity != advertising.RuleAdSet || r.ObjectIDs[0] != "120300" ||
		r.Window != advertising.WindowToday || r.Frequency != advertising.RuleEvery30Minutes || r.AdAccountID != adAccountUUID {
		t.Fatalf("result %+v rule %+v", result, r)
	}
}

func TestRuleChangesNeedARuleFromTheList(t *testing.T) {
	tools, s := growthTools()
	status := tools["set_ad_rule_status"]
	if err := growthValidate(status, map[string]interface{}{"ad_account_id": adAccountUUID, "rule_id": "1", "status": "DISABLED"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "rule_id": "777", "status": "DISABLED"}
	if fields := growthDescribe(status, args); fields["rule"] != "Pausar caro" || fields["change"] != "desligar" {
		t.Fatalf("fields %+v", fields)
	}
	status.Execute(context.Background(), adContext, args)
	tools["delete_ad_rule"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "rule_id": "777"})
	if len(s.rules.enabled) != 1 || s.rules.enabled[0] || len(s.rules.deleted) != 1 {
		t.Fatalf("enabled %+v deleted %+v", s.rules.enabled, s.rules.deleted)
	}
}

func TestRuleHistoryCountsChangedItems(t *testing.T) {
	tools, _ := growthTools()
	result := tools["ad_rule_history"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "rule_id": "777"})
	runs := result.Data.(map[string]interface{})["runs"].([]map[string]interface{})
	if result.Status != copilot.StatusOK || runs[0]["objects_changed"] != 2 || runs[0]["at"] != adTestClock.In(time.FixedZone("BRT", -3*3600)).Format("2006-01-02 15:04") {
		t.Fatalf("result %+v", result)
	}
	missing := tools["ad_rule_history"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "rule_id": "1"})
	if missing.Status != copilot.StatusError {
		t.Fatalf("missing %+v", missing)
	}
}
