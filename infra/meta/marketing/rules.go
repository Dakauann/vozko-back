package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.RuleGateway = (*Gateway)(nil)

const (
	ruleFields           = "id,name,status,evaluation_spec,execution_spec,schedule_spec,created_time"
	ruleHistoryFields    = "timestamp,execution_spec,results,is_manual"
	ruleEvaluationType   = "SCHEDULE"
	ruleEntityField      = "entity_type"
	ruleWindowField      = "time_preset"
	ruleIDField          = "id"
	ruleStatusField      = "effective_status"
	ruleChangeSpecField  = "change_spec"
	ruleBudgetUnit       = "PERCENTAGE"
	ruleCampaignBudget   = "CHANGE_CAMPAIGN_BUDGET"
	ruleOperatorEqual    = "EQUAL"
	ruleOperatorIn       = "IN"
	ruleDeliveryActive   = "ACTIVE"
	ruleDeliveryPaused   = "PAUSED"
	ruleObjectFieldShape = "%s.id"
)

var ruleMetricFields = map[advertising.RuleMetric]string{
	advertising.MetricSpent:         "spent",
	advertising.MetricResults:       "results",
	advertising.MetricCostPerResult: "cost_per_result",
	advertising.MetricImpressions:   "impressions",
	advertising.MetricReach:         "reach",
	advertising.MetricFrequency:     "frequency",
	advertising.MetricCPC:           "cpc",
	advertising.MetricCPM:           "cpm",
	advertising.MetricCTR:           "ctr",
}

var ruleWindowPresets = map[advertising.RuleWindow]string{
	advertising.WindowToday:      "TODAY",
	advertising.WindowYesterday:  "YESTERDAY",
	advertising.WindowLast3Days:  "LAST_3_DAYS",
	advertising.WindowLast7Days:  "LAST_7_DAYS",
	advertising.WindowLast14Days: "LAST_14_DAYS",
	advertising.WindowLast30Days: "LAST_30_DAYS",
	advertising.WindowLifetime:   "LIFETIME",
}

type graphRuleFilter struct {
	Field    string          `json:"field"`
	Value    json.RawMessage `json:"value"`
	Operator string          `json:"operator"`
}

type graphRuleOption struct {
	Field    string          `json:"field"`
	Value    json.RawMessage `json:"value"`
	Operator string          `json:"operator"`
}

type graphEvaluationSpec struct {
	EvaluationType string            `json:"evaluation_type"`
	Filters        []graphRuleFilter `json:"filters"`
	Trigger        *graphRuleFilter  `json:"trigger,omitempty"`
}

type graphExecutionSpec struct {
	ExecutionType    string            `json:"execution_type"`
	ExecutionOptions []graphRuleOption `json:"execution_options,omitempty"`
}

type graphScheduleSpec struct {
	ScheduleType string `json:"schedule_type"`
}

type graphChangeSpec struct {
	Amount json.Number `json:"amount"`
	Unit   string      `json:"unit"`
	Limit  json.Number `json:"limit,omitempty"`
}

func rawJSON(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func ruleFilter(field, operator string, value any) (graphRuleFilter, error) {
	raw, err := rawJSON(value)
	if err != nil {
		return graphRuleFilter{}, err
	}
	return graphRuleFilter{Field: field, Value: raw, Operator: operator}, nil
}

func ruleMoneyToMinor(currency string, amount float64) (int64, error) {
	micros, err := advertising.DecimalToMicros(strconv.FormatFloat(amount, 'f', 6, 64))
	if err != nil {
		return 0, err
	}
	return advertising.MicrosToMinor(currency, micros)
}

func ruleMinorToMoney(currency string, minor int64) (float64, error) {
	micros, err := advertising.MinorToMicros(currency, minor)
	if err != nil {
		return 0, err
	}
	return advertising.MicrosToAmount(micros), nil
}

func evaluationSpec(currency string, rule advertising.AutomatedRule) (graphEvaluationSpec, error) {
	preset, ok := ruleWindowPresets[rule.Window]
	if !ok {
		return graphEvaluationSpec{}, fmt.Errorf("marketing: rule window %q has no meta preset", rule.Window)
	}
	spec := graphEvaluationSpec{EvaluationType: ruleEvaluationType}
	add := func(field, operator string, value any) error {
		f, err := ruleFilter(field, operator, value)
		if err != nil {
			return err
		}
		spec.Filters = append(spec.Filters, f)
		return nil
	}
	if err := add(ruleEntityField, ruleOperatorEqual, string(rule.Entity)); err != nil {
		return graphEvaluationSpec{}, err
	}
	if err := add(ruleWindowField, ruleOperatorEqual, preset); err != nil {
		return graphEvaluationSpec{}, err
	}
	if len(rule.ObjectIDs) > 0 {
		if err := add(ruleIDField, ruleOperatorIn, rule.ObjectIDs); err != nil {
			return graphEvaluationSpec{}, err
		}
	}
	switch rule.Action.Type {
	case advertising.RuleActionPause:
		if err := add(ruleStatusField, ruleOperatorIn, []string{ruleDeliveryActive}); err != nil {
			return graphEvaluationSpec{}, err
		}
	case advertising.RuleActionUnpause:
		if err := add(ruleStatusField, ruleOperatorIn, []string{ruleDeliveryPaused}); err != nil {
			return graphEvaluationSpec{}, err
		}
	}
	for _, c := range rule.Conditions {
		field, ok := ruleMetricFields[c.Metric]
		if !ok {
			return graphEvaluationSpec{}, fmt.Errorf("marketing: rule metric %q has no meta field", c.Metric)
		}
		if c.Operator != advertising.OperatorGreaterThan && c.Operator != advertising.OperatorLessThan {
			return graphEvaluationSpec{}, fmt.Errorf("marketing: rule operator %q is not supported", c.Operator)
		}
		var value any = c.Value
		if c.Metric.Money() {
			minor, err := ruleMoneyToMinor(currency, c.Value)
			if err != nil {
				return graphEvaluationSpec{}, err
			}
			value = minor
		}
		if err := add(field, string(c.Operator), value); err != nil {
			return graphEvaluationSpec{}, err
		}
	}
	return spec, nil
}

func executionSpec(rule advertising.AutomatedRule) (graphExecutionSpec, error) {
	switch rule.Action.Type {
	case advertising.RuleActionPause, advertising.RuleActionUnpause:
		return graphExecutionSpec{ExecutionType: string(rule.Action.Type)}, nil
	case advertising.RuleActionChangeBudget:
		change := graphChangeSpec{Amount: json.Number(strconv.Itoa(rule.Action.BudgetPercent)), Unit: ruleBudgetUnit}
		if rule.Action.BudgetCap > 0 {
			change.Limit = json.Number(strconv.FormatInt(rule.Action.BudgetCap, 10))
		}
		raw, err := rawJSON(change)
		if err != nil {
			return graphExecutionSpec{}, err
		}
		executionType := string(advertising.RuleActionChangeBudget)
		if rule.Entity == advertising.RuleCampaign {
			executionType = ruleCampaignBudget
		}
		return graphExecutionSpec{
			ExecutionType:    executionType,
			ExecutionOptions: []graphRuleOption{{Field: ruleChangeSpecField, Value: raw, Operator: ruleOperatorEqual}},
		}, nil
	}
	return graphExecutionSpec{}, fmt.Errorf("marketing: rule action %q is not supported", rule.Action.Type)
}

func scheduleSpec(frequency advertising.RuleFrequency) (graphScheduleSpec, error) {
	switch frequency {
	case advertising.RuleEvery30Minutes, advertising.RuleHourly, advertising.RuleDaily:
		return graphScheduleSpec{ScheduleType: string(frequency)}, nil
	}
	return graphScheduleSpec{}, fmt.Errorf("marketing: rule frequency %q is not supported", frequency)
}

func ruleStatusValue(status advertising.RuleStatus) (string, error) {
	switch status {
	case advertising.RuleEnabled, advertising.RuleDisabled:
		return string(status), nil
	}
	return "", fmt.Errorf("marketing: rule status %q is not supported", status)
}

func (g *Gateway) CreateRule(ctx context.Context, token, metaAccountID, currency string, rule advertising.AutomatedRule) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	status, err := ruleStatusValue(rule.Status)
	if err != nil {
		return "", err
	}
	evaluation, err := evaluationSpec(currency, rule)
	if err != nil {
		return "", err
	}
	execution, err := executionSpec(rule)
	if err != nil {
		return "", err
	}
	schedule, err := scheduleSpec(rule.Frequency)
	if err != nil {
		return "", err
	}
	form := url.Values{"name": {rule.Name}, "status": {status}}
	for field, spec := range map[string]any{"evaluation_spec": evaluation, "execution_spec": execution, "schedule_spec": schedule} {
		encoded, err := jsonValue(spec)
		if err != nil {
			return "", err
		}
		form.Set(field, encoded)
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/adrules_library", Token: token, Form: form}, "automated rule")
}

type graphRule struct {
	ID          meta.GraphID        `json:"id"`
	Name        string              `json:"name"`
	Status      string              `json:"status"`
	Evaluation  graphEvaluationSpec `json:"evaluation_spec"`
	Execution   graphExecutionSpec  `json:"execution_spec"`
	Schedule    graphScheduleSpec   `json:"schedule_spec"`
	CreatedTime string              `json:"created_time"`
}

type ruleCurrency struct {
	g        *Gateway
	token    string
	path     string
	currency string
}

func (c *ruleCurrency) get(ctx context.Context) (string, error) {
	if c.currency != "" {
		return c.currency, nil
	}
	var out struct {
		Currency string `json:"currency"`
	}
	if err := c.g.do(ctx, meta.Request{Method: http.MethodGet, Path: c.path, Token: c.token, Query: url.Values{"fields": {"currency"}}}, &out); err != nil {
		return "", err
	}
	currency, err := advertising.NormalizeCurrency(out.Currency)
	if err != nil {
		return "", err
	}
	c.currency = currency
	return currency, nil
}

func (g *Gateway) ListRules(ctx context.Context, token, metaAccountID string) ([]advertising.AutomatedRule, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	rows, err := collect[graphRule](ctx, g, path+"/adrules_library", token, url.Values{"fields": {ruleFields}, "limit": {"100"}})
	if err != nil {
		return nil, err
	}
	currency := &ruleCurrency{g: g, token: token, path: path}
	rules := make([]advertising.AutomatedRule, 0, len(rows))
	for _, row := range rows {
		rule, err := row.toDomain(ctx, advertising.NormalizeAccountID(metaAccountID), currency)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (r graphRule) toDomain(ctx context.Context, accountID string, currency *ruleCurrency) (advertising.AutomatedRule, error) {
	if r.ID == "" {
		return advertising.AutomatedRule{}, fmt.Errorf("marketing: automated rule without an id")
	}
	created, err := graphTime("created_time", r.CreatedTime)
	if err != nil {
		return advertising.AutomatedRule{}, err
	}
	rule := advertising.AutomatedRule{
		MetaID:      r.ID.String(),
		AdAccountID: accountID,
		Name:        r.Name,
		Status:      advertising.RuleStatus(r.Status),
		Frequency:   advertising.RuleFrequency(r.Schedule.ScheduleType),
		CreatedTime: created,
	}
	if err := readEvaluation(ctx, &rule, r.Evaluation, currency); err != nil {
		return advertising.AutomatedRule{}, fmt.Errorf("marketing: rule %s: %w", r.ID, err)
	}
	if rule.Action, err = ruleActionOf(r.Execution); err != nil {
		return advertising.AutomatedRule{}, fmt.Errorf("marketing: rule %s: %w", r.ID, err)
	}
	return rule, nil
}

func readEvaluation(ctx context.Context, rule *advertising.AutomatedRule, spec graphEvaluationSpec, currency *ruleCurrency) error {
	filters := slices.Clone(spec.Filters)
	if spec.Trigger != nil {
		filters = append(filters, *spec.Trigger)
	}
	var rest []graphRuleFilter
	for _, f := range filters {
		switch f.Field {
		case ruleEntityField:
			var entity string
			if err := json.Unmarshal(f.Value, &entity); err != nil {
				return fmt.Errorf("entity_type is not text: %w", err)
			}
			rule.Entity = advertising.RuleEntity(entity)
		case ruleWindowField:
			var preset string
			if err := json.Unmarshal(f.Value, &preset); err != nil {
				return fmt.Errorf("time_preset is not text: %w", err)
			}
			rule.Window = ruleWindowOf(preset)
		case ruleStatusField:
		default:
			rest = append(rest, f)
		}
	}
	objectField := fmt.Sprintf(ruleObjectFieldShape, strings.ToLower(string(rule.Entity)))
	for _, f := range rest {
		if f.Field == ruleIDField || f.Field == objectField {
			var ids []meta.GraphID
			if err := json.Unmarshal(f.Value, &ids); err != nil {
				return fmt.Errorf("%s is not a list of ids: %w", f.Field, err)
			}
			for _, id := range ids {
				rule.ObjectIDs = append(rule.ObjectIDs, id.String())
			}
			continue
		}
		condition, err := ruleConditionOf(ctx, f, currency)
		if err != nil {
			return err
		}
		rule.Conditions = append(rule.Conditions, condition)
	}
	return nil
}

func ruleWindowOf(preset string) advertising.RuleWindow {
	for window, known := range ruleWindowPresets {
		if known == preset {
			return window
		}
	}
	return advertising.RuleWindow(preset)
}

func ruleMetricOf(field string) advertising.RuleMetric {
	for metric, known := range ruleMetricFields {
		if known == field {
			return metric
		}
	}
	return advertising.RuleMetric(field)
}

func ruleConditionOf(ctx context.Context, f graphRuleFilter, currency *ruleCurrency) (advertising.RuleCondition, error) {
	var raw graphNumber
	if err := json.Unmarshal(f.Value, &raw); err != nil {
		return advertising.RuleCondition{}, fmt.Errorf("filter %s value %s is not a number", f.Field, string(f.Value))
	}
	value, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return advertising.RuleCondition{}, fmt.Errorf("filter %s value %q is not a number", f.Field, string(raw))
	}
	metric := ruleMetricOf(f.Field)
	if metric.Valid() && metric.Money() {
		if value != math.Trunc(value) {
			return advertising.RuleCondition{}, fmt.Errorf("filter %s value %q is not in minor units", f.Field, string(raw))
		}
		code, err := currency.get(ctx)
		if err != nil {
			return advertising.RuleCondition{}, err
		}
		if value, err = ruleMinorToMoney(code, int64(value)); err != nil {
			return advertising.RuleCondition{}, err
		}
	}
	return advertising.RuleCondition{Metric: metric, Operator: advertising.RuleOperator(f.Operator), Value: value}, nil
}

func ruleActionOf(spec graphExecutionSpec) (advertising.RuleAction, error) {
	switch spec.ExecutionType {
	case string(advertising.RuleActionChangeBudget), ruleCampaignBudget:
	default:
		return advertising.RuleAction{Type: advertising.RuleActionType(spec.ExecutionType)}, nil
	}
	for _, option := range spec.ExecutionOptions {
		if option.Field != ruleChangeSpecField {
			continue
		}
		var change struct {
			Amount graphNumber `json:"amount"`
			Unit   string      `json:"unit"`
			Limit  graphNumber `json:"limit"`
		}
		if err := json.Unmarshal(option.Value, &change); err != nil {
			return advertising.RuleAction{}, fmt.Errorf("change_spec is not an object: %w", err)
		}
		if change.Unit != ruleBudgetUnit {
			return advertising.RuleAction{}, fmt.Errorf("change_spec unit %q is not supported", change.Unit)
		}
		percent, err := strconv.Atoi(string(change.Amount))
		if err != nil {
			return advertising.RuleAction{}, fmt.Errorf("change_spec amount %q is not a whole percent", string(change.Amount))
		}
		limit, err := change.Limit.minorUnits("change_spec limit")
		if err != nil {
			return advertising.RuleAction{}, err
		}
		return advertising.RuleAction{Type: advertising.RuleActionChangeBudget, BudgetPercent: percent, BudgetCap: limit}, nil
	}
	return advertising.RuleAction{}, fmt.Errorf("%s has no change_spec", spec.ExecutionType)
}

func (g *Gateway) SetRuleStatus(ctx context.Context, token, ruleID string, status advertising.RuleStatus) error {
	path, err := objectPath(ruleID)
	if err != nil {
		return err
	}
	value, err := ruleStatusValue(status)
	if err != nil {
		return err
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: token, Form: url.Values{"status": {value}}})
}

func (g *Gateway) DeleteRule(ctx context.Context, token, ruleID string) error {
	path, err := objectPath(ruleID)
	if err != nil {
		return err
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodDelete, Path: path, Token: token})
}

type graphRuleRun struct {
	Timestamp string             `json:"timestamp"`
	Execution graphExecutionSpec `json:"execution_spec"`
	Results   []struct {
		ObjectID meta.GraphID `json:"object_id"`
	} `json:"results"`
}

func (g *Gateway) RuleHistory(ctx context.Context, token, ruleID string) ([]advertising.RuleRun, error) {
	path, err := objectPath(ruleID)
	if err != nil {
		return nil, err
	}
	rows, err := collect[graphRuleRun](ctx, g, path+"/history", token, url.Values{"fields": {ruleHistoryFields}, "limit": {"100"}})
	if err != nil {
		return nil, err
	}
	runs := make([]advertising.RuleRun, 0, len(rows))
	for _, row := range rows {
		at, err := graphTime("timestamp", row.Timestamp)
		if err != nil {
			return nil, err
		}
		if at == nil {
			return nil, fmt.Errorf("marketing: rule %s history entry without a timestamp", ruleID)
		}
		run := advertising.RuleRun{At: *at, Result: row.Execution.ExecutionType}
		for _, result := range row.Results {
			if result.ObjectID != "" {
				run.Objects = append(run.Objects, result.ObjectID.String())
			}
		}
		runs = append(runs, run)
	}
	return runs, nil
}
