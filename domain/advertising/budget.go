package advertising

import (
	"fmt"
	"slices"
	"time"
)

type BudgetKind string

const (
	BudgetDaily    BudgetKind = "DAILY"
	BudgetLifetime BudgetKind = "LIFETIME"
)

type Budget struct {
	Kind   BudgetKind `json:"kind"`
	Amount int64      `json:"amount"`
}

func (b Budget) validate(v issues) {
	if b.Kind != BudgetDaily && b.Kind != BudgetLifetime {
		v.add("kind", "invalid")
	}
	if b.Amount <= 0 {
		v.add("amount", "must_be_positive")
	}
}

type BidStrategy string

const (
	BidLowestCost BidStrategy = BidLowestCostWithoutCap
	BidCap        BidStrategy = "LOWEST_COST_WITH_BID_CAP"
	BidCostCap    BidStrategy = "COST_CAP"
	BidMinROAS    BidStrategy = "LOWEST_COST_WITH_MIN_ROAS"
)

type Bid struct {
	Strategy  BidStrategy `json:"strategy"`
	Amount    int64       `json:"amount,omitempty"`
	ROASFloor float64     `json:"roasFloor,omitempty"`
}

func (b Bid) Normalized() Bid {
	if b.Strategy == "" {
		b.Strategy = BidLowestCost
	}
	return b
}

func (b Bid) validate(v issues, goal OptimizationGoal) {
	switch b.Strategy {
	case BidLowestCost:
		if b.Amount != 0 || b.ROASFloor != 0 {
			v.add("amount", "not_allowed")
		}
	case BidCap, BidCostCap:
		if b.Amount <= 0 {
			v.add("amount", "must_be_positive")
		}
	case BidMinROAS:
		if b.ROASFloor <= 0 || b.ROASFloor > 1000 {
			v.add("roasFloor", "invalid")
		}
		if goal != "" && goal != GoalValue {
			v.add("strategy", "roas_needs_value_goal")
		}
	default:
		v.add("strategy", "invalid")
	}
}

func (b Bid) NeedsAmount() bool { return b.Strategy == BidCap || b.Strategy == BidCostCap }

func (b Bid) validateOnCampaign(v issues) {
	if b.Strategy == BidMinROAS {
		v.add("strategy", "roas_on_ad_set_only")
	}
}

func (b Bid) validateCampaignEdit(v issues) {
	b.validateOnCampaign(v)
	if b.NeedsAmount() {
		v.add("amount", "set_on_ad_sets")
	}
}

func (b Bid) ROASFloorParam() int64 { return int64(b.ROASFloor * 10_000) }

type DayPart struct {
	Days        []int `json:"days"`
	StartMinute int   `json:"startMinute"`
	EndMinute   int   `json:"endMinute"`
}

const minutesPerDay = 24 * 60

func validateSchedule(v issues, parts []DayPart, budget *Budget) {
	if len(parts) == 0 {
		return
	}
	if budget == nil || budget.Kind != BudgetLifetime {
		v.add("", "needs_lifetime_budget")
	}
	for _, p := range parts {
		if len(p.Days) == 0 || slices.ContainsFunc(p.Days, func(d int) bool { return d < 0 || d > 6 }) {
			v.add("days", "invalid")
		}
		if p.StartMinute < 0 || p.EndMinute > minutesPerDay || p.EndMinute-p.StartMinute < 60 || p.StartMinute%60 != 0 || p.EndMinute%60 != 0 {
			v.add("hours", "invalid")
		}
	}
}

func validateFlight(v issues, start, end *time.Time, budget *Budget, now time.Time) {
	if budget != nil && budget.Kind == BudgetLifetime && end == nil {
		v.add("endAt", "required_for_lifetime_budget")
	}
	if end == nil {
		return
	}
	if !end.After(now) {
		v.add("endAt", "in_the_past")
	}
	if start != nil && !end.After(*start) {
		v.add("endAt", "before_start")
	}
}

func ValidateSpendCap(cap, amountSpent int64) error {
	if cap <= 0 {
		return fmt.Errorf("%w: spend cap must be positive", ErrInvalidBudget)
	}
	if cap <= amountSpent {
		return fmt.Errorf("%w: spend cap must be above what was already spent", ErrInvalidBudget)
	}
	return nil
}

type MinimumBudgets struct {
	Currency      string
	Account       int64
	Impressions   int64
	VideoViews    int64
	HighFrequency int64
	LowFrequency  int64
}

type BudgetMinimum struct {
	Field    string
	Daily    int64
	Currency string
}

const CodeBelowMinimum = "below_minimum"

func (m MinimumBudgets) DailyFor(goal OptimizationGoal) int64 {
	return max(m.Account, m.goalMinimum(goal))
}

func (m MinimumBudgets) goalMinimum(goal OptimizationGoal) int64 {
	switch goal {
	case GoalImpressions:
		return m.Impressions
	case GoalThruPlay:
		return m.VideoViews
	case GoalLinkClicks:
		return m.HighFrequency
	case GoalAppInstalls:
		return m.LowFrequency
	}
	return 0
}

func (m MinimumBudgets) For(goal OptimizationGoal) BudgetMinimum {
	return BudgetMinimum{Daily: m.DailyFor(goal), Currency: m.Currency}
}

func (m MinimumBudgets) Check(field string, b Budget, goal OptimizationGoal) (BudgetMinimum, error) {
	minimum := m.For(goal)
	minimum.Field = field
	if b.Kind != BudgetDaily || minimum.Daily <= 0 || b.Amount >= minimum.Daily {
		return minimum, nil
	}
	return minimum, &ValidationError{Issues: []FieldIssue{{Field: field, Code: CodeBelowMinimum}}, Minimum: &minimum}
}

func (b Bid) ManualAmount() int64 {
	if b.NeedsAmount() {
		return b.Amount
	}
	return 0
}
