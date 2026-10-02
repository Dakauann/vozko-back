package advertising

import (
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrRuleNotFound = errors.New("automated rule not found")

type RuleEntity string

const (
	RuleCampaign RuleEntity = "CAMPAIGN"
	RuleAdSet    RuleEntity = "ADSET"
	RuleAd       RuleEntity = "AD"
)

type RuleMetric string

const (
	MetricSpent         RuleMetric = "spent"
	MetricResults       RuleMetric = "results"
	MetricCostPerResult RuleMetric = "cost_per_result"
	MetricImpressions   RuleMetric = "impressions"
	MetricReach         RuleMetric = "reach"
	MetricFrequency     RuleMetric = "frequency"
	MetricCPC           RuleMetric = "cpc"
	MetricCPM           RuleMetric = "cpm"
	MetricCTR           RuleMetric = "ctr"
)

var moneyMetrics = []RuleMetric{MetricSpent, MetricCostPerResult, MetricCPC, MetricCPM}

var ruleMetrics = []RuleMetric{MetricSpent, MetricResults, MetricCostPerResult, MetricImpressions, MetricReach, MetricFrequency, MetricCPC, MetricCPM, MetricCTR}

func RuleMetrics() []RuleMetric { return append([]RuleMetric(nil), ruleMetrics...) }

func (m RuleMetric) Valid() bool { return slices.Contains(ruleMetrics, m) }

func (m RuleMetric) Money() bool { return slices.Contains(moneyMetrics, m) }

type RuleOperator string

const (
	OperatorGreaterThan RuleOperator = "GREATER_THAN"
	OperatorLessThan    RuleOperator = "LESS_THAN"
)

type RuleCondition struct {
	Metric   RuleMetric   `json:"metric"`
	Operator RuleOperator `json:"operator"`
	Value    float64      `json:"value"`
}

type RuleWindow string

const (
	WindowToday      RuleWindow = "TODAY"
	WindowYesterday  RuleWindow = "YESTERDAY"
	WindowLast3Days  RuleWindow = "LAST_3_DAYS"
	WindowLast7Days  RuleWindow = "LAST_7_DAYS"
	WindowLast14Days RuleWindow = "LAST_14_DAYS"
	WindowLast30Days RuleWindow = "LAST_30_DAYS"
	WindowLifetime   RuleWindow = "LIFETIME"
)

var ruleWindows = []RuleWindow{WindowToday, WindowYesterday, WindowLast3Days, WindowLast7Days, WindowLast14Days, WindowLast30Days, WindowLifetime}

type RuleActionType string

const (
	RuleActionPause        RuleActionType = "PAUSE"
	RuleActionUnpause      RuleActionType = "UNPAUSE"
	RuleActionChangeBudget RuleActionType = "CHANGE_BUDGET"
)

type RuleAction struct {
	Type          RuleActionType `json:"type"`
	BudgetPercent int            `json:"budgetPercent,omitempty"`
	BudgetCap     int64          `json:"budgetCap,omitempty"`
}

type RuleFrequency string

const (
	RuleEvery30Minutes RuleFrequency = "SEMI_HOURLY"
	RuleHourly         RuleFrequency = "HOURLY"
	RuleDaily          RuleFrequency = "DAILY"
)

type RuleStatus string

const (
	RuleEnabled  RuleStatus = "ENABLED"
	RuleDisabled RuleStatus = "DISABLED"
)

type AutomatedRule struct {
	MetaID      string          `json:"metaId,omitempty"`
	AdAccountID string          `json:"adAccountId"`
	Name        string          `json:"name"`
	Status      RuleStatus      `json:"status"`
	Entity      RuleEntity      `json:"entity"`
	ObjectIDs   []string        `json:"objectIds,omitempty"`
	Conditions  []RuleCondition `json:"conditions"`
	Window      RuleWindow      `json:"window"`
	Action      RuleAction      `json:"action"`
	Frequency   RuleFrequency   `json:"frequency"`
	CreatedTime *time.Time      `json:"createdTime,omitempty"`
}

type RuleRun struct {
	At      time.Time `json:"at"`
	Result  string    `json:"result"`
	Objects []string  `json:"objects,omitempty"`
}

const (
	maxRuleConditions = 5
	maxBudgetPercent  = 100
)

func (r *AutomatedRule) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	if r.Status == "" {
		r.Status = RuleEnabled
	}
	if r.Frequency == "" {
		r.Frequency = RuleEvery30Minutes
	}
	if r.Window == "" {
		r.Window = WindowToday
	}
}

func (r AutomatedRule) Validate() error {
	v := newIssues()
	if strings.TrimSpace(r.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	v.text("name", r.Name, true, maxNameRunes)
	if r.Entity != RuleCampaign && r.Entity != RuleAdSet && r.Entity != RuleAd {
		v.add("entity", "invalid")
	}
	if r.Status != RuleEnabled && r.Status != RuleDisabled {
		v.add("status", "invalid")
	}
	if !slices.Contains(ruleWindows, r.Window) {
		v.add("window", "invalid")
	}
	if r.Frequency != RuleEvery30Minutes && r.Frequency != RuleHourly && r.Frequency != RuleDaily {
		v.add("frequency", "invalid")
	}
	switch n := len(r.Conditions); {
	case n == 0:
		v.add("conditions", "required")
	case n > maxRuleConditions:
		v.add("conditions", "too_many")
	}
	for i, c := range r.Conditions {
		cv := v.item("conditions", i)
		if !c.Metric.Valid() {
			cv.add("metric", "invalid")
		}
		if c.Operator != OperatorGreaterThan && c.Operator != OperatorLessThan {
			cv.add("operator", "invalid")
		}
		if c.Value < 0 {
			cv.add("value", "invalid")
		}
	}
	switch r.Action.Type {
	case RuleActionPause, RuleActionUnpause:
	case RuleActionChangeBudget:
		if r.Entity == RuleAd {
			v.at("action").add("type", "not_for_ads")
		}
		if r.Action.BudgetPercent == 0 || r.Action.BudgetPercent < -maxBudgetPercent || r.Action.BudgetPercent > maxBudgetPercent {
			v.at("action").add("budgetPercent", "invalid")
		}
	default:
		v.at("action").add("type", "invalid")
	}
	return v.err()
}
