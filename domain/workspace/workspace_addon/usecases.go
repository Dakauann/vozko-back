package workspace_addon

import (
	"time"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type AddonDefinitionMutationInput struct {
	Key                string          `json:"key"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	EntitlementKind    EntitlementKind `json:"entitlementKind"`
	UnitsPerQuantity   int             `json:"unitsPerQuantity"`
	MonthlyPriceMicros int64           `json:"monthlyPriceMicros"`
	AnnualPriceMicros  int64           `json:"annualPriceMicros"`
	MonthlyCostMicros  int64           `json:"monthlyCostMicros"`
	AnnualCostMicros   int64           `json:"annualCostMicros"`
	IsActive           *bool           `json:"isActive,omitempty"`
	IsGloballyVisible  *bool           `json:"isGloballyVisible,omitempty"`
}

type CreateAddonDefinitionUseCase interface {
	Execute(input AddonDefinitionMutationInput) (*AddonDefinition, error)
}

type UpdateAddonDefinitionUseCase interface {
	Execute(id string, input AddonDefinitionMutationInput) (*AddonDefinition, error)
}

type ArchiveAddonDefinitionUseCase interface {
	Execute(id string) error
}

type ListAddonDefinitionsUseCase interface {
	Execute(includeArchived bool) ([]*AddonDefinition, error)
}

type GetAddonDefinitionUseCase interface {
	Execute(id string) (*AddonDefinition, error)
}

type ListAvailableAddonsUseCase interface {
	Execute(workspaceID string) ([]*AddonDefinition, error)
}

type PurchaseAddonInput struct {
	AddonDefinitionID string                      `json:"addonDefinitionId"`
	Quantity          int                         `json:"quantity"`
	BillingCycle      workspace_plan.BillingCycle `json:"billingCycle"`
}

type PurchaseAddonUseCase interface {
	Execute(workspaceID string, input PurchaseAddonInput) (*AddonSubscription, error)
}

// AddonPurchasePreview is the read-only, no-charge quote shown before a customer confirms an addon
// purchase: the exact amount charged up front (the prorated activation stub for a new monthly channel),
// the steady-state recurring amount, and the proration math (days + whether it also covers next month).
// All money is in USD micros (the saldo currency); the frontend converts to BRL for display. Price only.
type AddonPurchasePreview struct {
	ChargeNowMicros int64                       `json:"chargeNowMicros"`
	RecurringMicros int64                       `json:"recurringMicros"`
	BillingCycle    workspace_plan.BillingCycle `json:"billingCycle"`
	Prorated        bool                        `json:"prorated"`
	ProratedDays    int                         `json:"proratedDays"`
	PeriodEnd       time.Time                   `json:"periodEnd"`
	NextInvoiceDate time.Time                   `json:"nextInvoiceDate"`
}

// PreviewAddonPurchaseUseCase computes AddonPurchasePreview without charging or persisting anything, so
// the UI can show the exact "you pay now" amount before purchase. It uses the same billing.ActivationPeriod
// as PurchaseAddonUseCase, so the quote always equals the charge.
type PreviewAddonPurchaseUseCase interface {
	Execute(workspaceID string, input PurchaseAddonInput) (*AddonPurchasePreview, error)
}

type CancelAddonSubscriptionUseCase interface {
	Execute(workspaceID, subscriptionID string) (*AddonSubscription, error)
}

type ListWorkspaceAddonsUseCase interface {
	Execute(workspaceID string) ([]*AddonSubscription, error)
}

type WorkspaceEntitlement struct {
	Kind       EntitlementKind `json:"kind"`
	PlanBase   int             `json:"planBase"`
	AddonUnits int             `json:"addonUnits"`
	Total      int             `json:"total"`
}

type GetWorkspaceEntitlementsUseCase interface {
	Execute(workspaceID string) ([]WorkspaceEntitlement, error)
}
