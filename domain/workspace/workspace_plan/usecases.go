package workspace_plan

import "vozko/domain/invoice"

type PlanPricingItemInput struct {
	Category    string  `json:"category"`
	Service     string  `json:"service"`
	Metric      string  `json:"metric"`
	CostMicros  int64   `json:"costMicros"`
	PriceMicros int64   `json:"priceMicros"`
	MarkupPct   float64 `json:"markupPct"`
	Currency    string  `json:"currency"`
}

type PlanMutationInput struct {
	Name                           string                 `json:"name"`
	Description                    string                 `json:"description"`
	BasePriceBRLCents              int64                  `json:"basePriceBRLCents"`
	MaxCallChannels                int                    `json:"maxCallChannels"`
	IncludedWhatsAppBusinessPhones int                    `json:"includedWhatsAppBusinessPhones,omitempty"`
	MaxBranches                    int                    `json:"maxBranches,omitempty"`
	MaxHoldMusicTracks             int                    `json:"maxHoldMusicTracks,omitempty"`
	IsGloballyVisible              *bool                  `json:"isGloballyVisible,omitempty"`
	PricingItems                   []PlanPricingItemInput `json:"pricingItems"`
}

type CreatePlanDefinitionUseCase interface {
	Execute(input PlanMutationInput) (*PlanDefinition, error)
}

type UpdatePlanDefinitionUseCase interface {
	Execute(planID string, input PlanMutationInput) (*PlanDefinition, error)
}

type ArchivePlanDefinitionUseCase interface {
	Execute(planID string) error
}

type ListPlanDefinitionsUseCase interface {
	Execute(includeArchived bool) ([]*PlanDefinition, error)
}

type GetPlanDefinitionUseCase interface {
	Execute(planID string) (*PlanDefinition, error)
}

type CreateSubscriptionInvoiceInput struct {
	PlanID       string `json:"planId"`
	BillingType  string `json:"billingType"`
	BillingCycle string `json:"billingCycle"`
	Description  string `json:"description"`

	ReferralCode string `json:"referralCode"`
}

type CreateSubscriptionInvoiceUseCase interface {
	Execute(workspaceID, userID string, input CreateSubscriptionInvoiceInput) (*invoice.CreateInvoiceOutput, error)
}

type SubscribeWorkspaceUseCase interface {
	Execute(workspaceID, planID string, billingCycle BillingCycle) (*WorkspaceSubscriptionDetails, error)
}

type RenewWorkspaceSubscriptionUseCase interface {
	Execute(workspaceID string) (*WorkspaceSubscriptionDetails, error)
}

type CancelWorkspaceSubscriptionUseCase interface {
	Execute(workspaceID string) (*WorkspaceSubscriptionDetails, error)
}

type GetWorkspaceSubscriptionUseCase interface {
	Execute(workspaceID string) (*WorkspaceSubscriptionDetails, error)
}

type EnsureCurrentWorkspaceSubscriptionUseCase interface {
	Execute(workspaceID string) (*WorkspaceSubscription, error)
}

type EnsureActiveWorkspaceSubscriptionUseCase interface {
	Execute(workspaceID string) (*WorkspaceSubscription, error)
}

type ExpireSubscriptionsUseCase interface {
	Execute() (int64, error)
}

// RemindExpiringSubscriptionsUseCase emails owners whose plan expires within the
// reminder lead time. Idempotent per period (safe to run daily).
type RemindExpiringSubscriptionsUseCase interface {
	RemindExpiring() (int, error)
}

type SetPlanVisibilityInput struct {
	IsGloballyVisible   bool     `json:"isGloballyVisible"`
	AllowedWorkspaceIDs []string `json:"allowedWorkspaceIds"`
}

type SetPlanVisibilityUseCase interface {
	Execute(planID string, input SetPlanVisibilityInput) (*PlanDefinition, error)
}

type ListVisiblePlansUseCase interface {
	Execute(workspaceID string) ([]*PlanDefinition, error)
}

type SetPlanExclusiveAffiliateInput struct {
	AffiliateID *string `json:"affiliateId"`
}

type SetPlanExclusiveAffiliateUseCase interface {
	Execute(planID string, input SetPlanExclusiveAffiliateInput) (*PlanDefinition, error)
}

type ListExclusivePlansByAffiliateCodeUseCase interface {
	Execute(code string) ([]*PlanDefinition, error)
}

type ListMyExclusivePlansUseCase interface {
	Execute(userID string) ([]*PlanDefinition, error)
}
