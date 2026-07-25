package workspace_plan

import (
	"strings"
	"time"

	billing "vozko/domain/billing"
)

type SubscriptionStatus string

const (
	SubscriptionStatusActive    SubscriptionStatus = "active"
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled"
	SubscriptionStatusExpired   SubscriptionStatus = "expired"
)

func (s SubscriptionStatus) IsValid() bool {
	switch s {
	case SubscriptionStatusActive, SubscriptionStatusCancelled, SubscriptionStatusExpired:
		return true
	default:
		return false
	}
}

type BillingCycle string

const (
	BillingCycleMonthly BillingCycle = "monthly"
	BillingCycleAnnual  BillingCycle = "annual"
)

const AnnualDiscountPct = 0

func (c BillingCycle) IsValid() bool {
	switch c {
	case BillingCycleMonthly, BillingCycleAnnual:
		return true
	default:
		return false
	}
}

func (c BillingCycle) PeriodMonths() int {
	if c == BillingCycleAnnual {
		return 12
	}
	return 1
}

func (c BillingCycle) TotalPriceBRLCents(monthlyBaseCents int64) int64 {
	months := int64(c.PeriodMonths())
	if AnnualDiscountPct > 0 && c == BillingCycleAnnual {
		discounted := monthlyBaseCents * (100 - AnnualDiscountPct) / 100
		return discounted * months
	}
	return monthlyBaseCents * months
}

type PlanDefinition struct {
	ID                             string `json:"id"`
	Name                           string `json:"name"`
	Description                    string `json:"description"`
	BasePriceBRLCents              int64  `json:"basePriceBRLCents"`
	MaxCallChannels                int    `json:"maxCallChannels"`
	IncludedWhatsAppBusinessPhones int    `json:"includedWhatsAppBusinessPhones"`
	// MaxBranches is how many member SIP extensions (branches) this plan grants.
	// 0 means none included (the create gate fails closed until it is set, or an
	// addon tops it up). Resolved via EntitlementBranches, exactly like
	// MaxCallChannels via EntitlementCallChannels.
	MaxBranches int `json:"maxBranches"`
	// MaxHoldMusicTracks is how many CUSTOM hold music tracks this plan lets a
	// workspace keep uploaded (builtins are always available). 0 disables custom
	// uploads for the plan; the media layer hard-caps the effective value at 10.
	// Resolved via EntitlementHoldMusicTracks.
	MaxHoldMusicTracks int  `json:"maxHoldMusicTracks"`
	IsGloballyVisible  bool `json:"isGloballyVisible"`

	ExclusiveAffiliateID *string            `json:"exclusiveAffiliateId,omitempty"`
	PricingItems         []PlanPricingItem  `json:"pricingItems,omitempty"`
	AllowedWorkspaceIDs  []string           `json:"allowedWorkspaceIds,omitempty" gorm:"-"`
	AllowedWorkspaces    []AllowedWorkspace `json:"allowedWorkspaces,omitempty" gorm:"-"`
	ArchivedAt           *time.Time         `json:"archivedAt,omitempty"`
	CreatedAt            time.Time          `json:"createdAt"`
	UpdatedAt            time.Time          `json:"updatedAt"`
}

type PlanPricingItem struct {
	ID               string    `json:"id"`
	PlanDefinitionID string    `json:"planDefinitionId"`
	Category         string    `json:"category"`
	Service          string    `json:"service"`
	Metric           string    `json:"metric"`
	CostMicros       int64     `json:"costMicros"`
	PriceMicros      int64     `json:"priceMicros"`
	MarkupPct        float64   `json:"markupPct"`
	Currency         string    `json:"currency"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (p *PlanDefinition) Validate() error {
	if p == nil {
		return ErrPlanNotFound
	}
	if strings.TrimSpace(p.Name) == "" {
		return ErrInvalidPlanName
	}
	if p.BasePriceBRLCents < 0 {
		return ErrInvalidBasePrice
	}
	if p.MaxCallChannels <= 0 {
		return ErrInvalidMaxCallChannels
	}
	return nil
}

func (p *PlanDefinition) IsArchived() bool {
	return p != nil && p.ArchivedAt != nil
}

func (p *PlanDefinition) IsExclusive() bool {
	return p != nil && p.ExclusiveAffiliateID != nil && *p.ExclusiveAffiliateID != ""
}

func (p *PlanDefinition) IsVisibleTo(workspaceID string) bool {
	if p == nil || p.IsExclusive() {
		return false
	}
	if p.IsGloballyVisible {
		return true
	}
	for _, id := range p.AllowedWorkspaceIDs {
		if id == workspaceID {
			return true
		}
	}
	return false
}

func (p *PlanDefinition) IsVisibleToAffiliate(affiliateID string) bool {
	if p == nil || !p.IsExclusive() || affiliateID == "" {
		return false
	}
	return *p.ExclusiveAffiliateID == affiliateID
}

type PlanVisibilityEntry struct {
	PlanDefinitionID string    `json:"planDefinitionId"`
	WorkspaceID      string    `json:"workspaceId"`
	CreatedAt        time.Time `json:"createdAt"`
}

type AllowedWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CatalogEntry struct {
	Category    string
	Service     string
	Metric      string
	CostMicros  int64
	PriceMicros int64
	MarkupPct   float64
	Currency    string
}

func catalogItemKey(category, service, metric string) string {
	return category + "|" + service + "|" + metric
}

func SeedPricingItemsFromCatalog(planID string, catalog []CatalogEntry) []PlanPricingItem {
	items := make([]PlanPricingItem, 0, len(catalog))
	for _, c := range catalog {
		if c.Category == "exchange_rate" {
			continue
		}
		items = append(items, PlanPricingItem{
			PlanDefinitionID: planID,
			Category:         c.Category,
			Service:          c.Service,
			Metric:           c.Metric,
			CostMicros:       c.CostMicros,
			PriceMicros:      c.PriceMicros,
			MarkupPct:        c.MarkupPct,
			Currency:         c.Currency,
		})
	}
	return items
}

func MergePricingItemsWithCatalog(planID string, stored []PlanPricingItem, catalog []CatalogEntry) []PlanPricingItem {

	filtered := make([]PlanPricingItem, 0, len(stored))
	for _, item := range stored {
		if item.Category == "exchange_rate" {
			continue
		}
		filtered = append(filtered, item)
	}

	existing := make(map[string]struct{}, len(filtered))
	for _, item := range filtered {
		existing[catalogItemKey(item.Category, item.Service, item.Metric)] = struct{}{}
	}

	merged := make([]PlanPricingItem, len(filtered))
	copy(merged, filtered)

	for _, c := range catalog {
		if c.Category == "exchange_rate" {
			continue
		}
		k := catalogItemKey(c.Category, c.Service, c.Metric)
		if _, ok := existing[k]; ok {
			continue
		}
		merged = append(merged, PlanPricingItem{
			PlanDefinitionID: planID,
			Category:         c.Category,
			Service:          c.Service,
			Metric:           c.Metric,
			CostMicros:       c.CostMicros,
			PriceMicros:      c.PriceMicros,
			MarkupPct:        c.MarkupPct,
			Currency:         c.Currency,
		})
	}
	return merged
}

type WorkspaceSubscription struct {
	ID                 string             `json:"id"`
	WorkspaceID        string             `json:"workspaceId"`
	PlanDefinitionID   string             `json:"planDefinitionId"`
	PlanName           string             `json:"planName"`
	MaxCallChannels    int                `json:"maxCallChannels"`
	BillingCycle       BillingCycle       `json:"billingCycle"`
	Status             SubscriptionStatus `json:"status"`
	CurrentPeriodStart time.Time          `json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time          `json:"currentPeriodEnd"`
	CancelledAt        *time.Time         `json:"cancelledAt,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

func (s *WorkspaceSubscription) Validate() error {
	if s == nil {
		return ErrInvalidSubscription
	}
	if strings.TrimSpace(s.WorkspaceID) == "" || strings.TrimSpace(s.PlanDefinitionID) == "" {
		return ErrInvalidSubscription
	}
	if !s.Status.IsValid() {
		return ErrInvalidSubscription
	}
	if s.CurrentPeriodStart.IsZero() || s.CurrentPeriodEnd.IsZero() || !s.CurrentPeriodEnd.After(s.CurrentPeriodStart) {
		return ErrInvalidSubscription
	}
	return nil
}

func (s *WorkspaceSubscription) IsCurrent(at time.Time) bool {
	if s == nil {
		return false
	}
	if s.Status != SubscriptionStatusActive && s.Status != SubscriptionStatusCancelled {
		return false
	}
	return !at.After(s.CurrentPeriodEnd)
}

func (s *WorkspaceSubscription) ExpireIfNeeded(at time.Time) bool {
	if s == nil {
		return false
	}
	if s.Status == SubscriptionStatusExpired {
		return false
	}
	if at.After(s.CurrentPeriodEnd) {
		s.Status = SubscriptionStatusExpired
		s.UpdatedAt = at
		return true
	}
	return false
}

// Extend advances the plan subscription to its next billing period when a monthly charge is
// confirmed. Monthly subscriptions roll to the next global anchor (dueDay) computed in the billing
// timezone, so the plan stays co-termed to one unified date; annual subscriptions roll 12 months.
// Continuity is preserved from the prior period end, and a lapsed subscription advances from `from`.
func (s *WorkspaceSubscription) Extend(from time.Time, dueDay int) {
	if s == nil {
		return
	}
	start := s.CurrentPeriodEnd
	if start.Before(from) {
		start = from
	}
	s.CurrentPeriodStart = start
	if s.BillingCycle == BillingCycleAnnual {
		s.CurrentPeriodEnd = start.AddDate(0, 12, 0)
	} else {
		s.CurrentPeriodEnd = billing.NextAnchor(start.In(billing.LocationBRT()).AddDate(0, 0, 1), dueDay)
	}
	s.Status = SubscriptionStatusActive
	s.UpdatedAt = from
}

type WorkspaceSubscriptionDetails struct {
	Subscription *WorkspaceSubscription `json:"subscription"`
	Plan         *PlanDefinition        `json:"plan"`
}

type PlanRepository interface {
	Create(plan *PlanDefinition) error
	Update(plan *PlanDefinition) error
	Archive(planID string, archivedAt time.Time) error
	GetByID(planID string) (*PlanDefinition, error)
	List(includeArchived bool) ([]*PlanDefinition, error)
	ReplacePricingItems(planID string, items []PlanPricingItem) error
	ListPricingItems(planID string) ([]PlanPricingItem, error)

	SetVisibility(planID string, workspaceIDs []string) error

	GetAllowedWorkspaceIDs(planID string) ([]string, error)

	GetAllowedWorkspaces(planID string) ([]AllowedWorkspace, error)

	ListVisiblePlans(workspaceID string) ([]*PlanDefinition, error)

	SetExclusiveAffiliate(planID string, affiliateID *string) error

	ListByExclusiveAffiliateID(affiliateID string) ([]*PlanDefinition, error)
}

type PlanReader interface {
	GetByID(planID string) (*PlanDefinition, error)
}

type CurrentSubscriptionReader interface {
	GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*WorkspaceSubscription, error)
}

type WorkspaceReferralReader interface {
	GetAffiliateIDByWorkspaceID(workspaceID string) (string, error)
}

type SubscriptionRepository interface {
	Create(subscription *WorkspaceSubscription) error
	Update(subscription *WorkspaceSubscription) error
	GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*WorkspaceSubscription, error)
	GetLatestByWorkspaceID(workspaceID string) (*WorkspaceSubscription, error)
	GetCurrentByWorkspaceIDs(workspaceIDs []string, at time.Time) (map[string]*WorkspaceSubscription, error)

	// ListUpcomingExpirations returns active or cancelled subscriptions whose period
	// ends in the (from, to] window, for the "plan expires soon" reminder.
	ListUpcomingExpirations(from, to time.Time, batchSize int) ([]*WorkspaceSubscription, error)

	// ListActiveBillingDue returns active subscriptions whose current period ends at or
	// before throughPeriodEnd, ordered by id and starting strictly after afterID (empty
	// for the first page), up to limit rows. Keyset pagination lets the monthly emitter
	// stream through every due workspace without an arbitrary per-run cap.
	ListActiveBillingDue(throughPeriodEnd time.Time, afterID string, limit int) ([]*WorkspaceSubscription, error)

	// ExpireOverdue marks overdue subscriptions expired and returns the workspace
	// id of each expired row (one entry per row, so len is the count). Callers use
	// the ids to reconcile dependent entitlements (e.g. suspend plan-included
	// WhatsApp numbers once the plan that funded them lapses).
	ExpireOverdue(at time.Time, batchSize int) ([]string, error)
}
