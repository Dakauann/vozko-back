package workspace_addon

import (
	"strings"
	"time"

	billing "vozko/domain/billing"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type AddonDefinition struct {
	ID                 string          `json:"id"`
	Key                string          `json:"key"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	EntitlementKind    EntitlementKind `json:"entitlementKind"`
	UnitsPerQuantity   int             `json:"unitsPerQuantity"`
	MonthlyPriceMicros int64           `json:"monthlyPriceMicros"`
	AnnualPriceMicros  int64           `json:"annualPriceMicros"`
	MonthlyCostMicros  int64           `json:"monthlyCostMicros"`
	AnnualCostMicros   int64           `json:"annualCostMicros"`
	IsActive           bool            `json:"isActive"`
	IsGloballyVisible  bool            `json:"isGloballyVisible"`
	ArchivedAt         *time.Time      `json:"archivedAt,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

func (a *AddonDefinition) Validate() error {
	if a == nil {
		return ErrAddonNotFound
	}
	if strings.TrimSpace(a.Key) == "" {
		return ErrInvalidAddonKey
	}
	if strings.TrimSpace(a.Name) == "" {
		return ErrInvalidAddonName
	}
	if !a.EntitlementKind.IsValid() {
		return ErrInvalidEntitlementKind
	}
	if a.UnitsPerQuantity <= 0 {
		return ErrInvalidUnitsPerQuantity
	}
	if a.MonthlyPriceMicros < 0 || a.AnnualPriceMicros < 0 || a.MonthlyCostMicros < 0 || a.AnnualCostMicros < 0 {
		return ErrInvalidAddonPrice
	}
	return nil
}

func (a *AddonDefinition) IsArchived() bool {
	return a != nil && a.ArchivedAt != nil
}

func (a *AddonDefinition) PriceMicros(cycle workspace_plan.BillingCycle) int64 {
	if cycle == workspace_plan.BillingCycleAnnual {
		return a.AnnualPriceMicros
	}
	return a.MonthlyPriceMicros
}

func (a *AddonDefinition) CostMicros(cycle workspace_plan.BillingCycle) int64 {
	if cycle == workspace_plan.BillingCycleAnnual {
		return a.AnnualCostMicros
	}
	return a.MonthlyCostMicros
}

type AddonSubscription struct {
	ID                 string                            `json:"id"`
	WorkspaceID        string                            `json:"workspaceId"`
	AddonDefinitionID  string                            `json:"addonDefinitionId"`
	AddonKey           string                            `json:"addonKey"`
	EntitlementKind    EntitlementKind                   `json:"entitlementKind"`
	Quantity           int                               `json:"quantity"`
	UnitsPerQuantity   int                               `json:"unitsPerQuantity"`
	BillingCycle       workspace_plan.BillingCycle       `json:"billingCycle"`
	Status             workspace_plan.SubscriptionStatus `json:"status"`
	UnitPriceMicros    int64                             `json:"unitPriceMicros"`
	BoundResourceType  *string                           `json:"boundResourceType,omitempty"`
	BoundResourceID    *string                           `json:"boundResourceId,omitempty"`
	CurrentPeriodStart time.Time                         `json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time                         `json:"currentPeriodEnd"`
	CancelledAt        *time.Time                        `json:"cancelledAt,omitempty"`
	CreatedAt          time.Time                         `json:"createdAt"`
	UpdatedAt          time.Time                         `json:"updatedAt"`
}

func (s *AddonSubscription) GrantedUnits() int {
	if s == nil {
		return 0
	}
	if s.Quantity <= 0 || s.UnitsPerQuantity <= 0 {
		return 0
	}
	return s.Quantity * s.UnitsPerQuantity
}

func (s *AddonSubscription) IsActive() bool {
	return s != nil && s.Status == workspace_plan.SubscriptionStatusActive
}

func (s *AddonSubscription) Validate() error {
	if s == nil {
		return ErrInvalidAddonSubscription
	}
	if strings.TrimSpace(s.WorkspaceID) == "" || strings.TrimSpace(s.AddonDefinitionID) == "" {
		return ErrInvalidAddonSubscription
	}
	if !s.EntitlementKind.IsValid() {
		return ErrInvalidAddonSubscription
	}
	if s.Quantity <= 0 || s.UnitsPerQuantity <= 0 {
		return ErrInvalidQuantity
	}
	if s.CurrentPeriodStart.IsZero() || s.CurrentPeriodEnd.IsZero() || !s.CurrentPeriodEnd.After(s.CurrentPeriodStart) {
		return ErrInvalidAddonSubscription
	}
	return nil
}

func (s *AddonSubscription) ExpireIfNeeded(at time.Time) bool {
	if s == nil {
		return false
	}
	if s.Status == workspace_plan.SubscriptionStatusExpired {
		return false
	}
	if at.After(s.CurrentPeriodEnd) {
		s.Status = workspace_plan.SubscriptionStatusExpired
		s.UpdatedAt = at
		return true
	}
	return false
}

// Extend advances the subscription to its next billing period. Monthly subscriptions roll to the
// next global billing anchor (dueDay, e.g. the 23rd) computed in the billing timezone, so a channel
// stays co-termed to one unified monthly date even when payment lands mid-dunning. Annual
// subscriptions roll 12 months. Continuity is preserved from the prior period end, and a long-lapsed
// subscription advances from `from` rather than back-dating.
func (s *AddonSubscription) Extend(from time.Time, dueDay int) {
	if s == nil {
		return
	}
	start := s.CurrentPeriodEnd
	if start.Before(from) {
		start = from
	}
	s.CurrentPeriodStart = start
	if s.BillingCycle == workspace_plan.BillingCycleAnnual {
		s.CurrentPeriodEnd = start.AddDate(0, 12, 0)
	} else {
		s.CurrentPeriodEnd = billing.NextAnchor(start.In(billing.LocationBRT()).AddDate(0, 0, 1), dueDay)
	}
	s.Status = workspace_plan.SubscriptionStatusActive
	s.UpdatedAt = from
}
