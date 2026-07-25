package workspace_addon_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/infra/database/schema"
)

type addonSubscriptionRepository struct {
	db *gorm.DB
}

func NewAddonSubscriptionRepository(db *gorm.DB) workspace_addon.AddonSubscriptionRepository {
	return &addonSubscriptionRepository{db: db}
}

func (r *addonSubscriptionRepository) Create(sub *workspace_addon.AddonSubscription) error {
	if sub == nil {
		return workspace_addon.ErrInvalidAddonSubscription
	}
	return r.db.Create(subscriptionToSchema(sub)).Error
}

func (r *addonSubscriptionRepository) Update(sub *workspace_addon.AddonSubscription) error {
	if sub == nil {
		return workspace_addon.ErrInvalidAddonSubscription
	}
	result := r.db.Model(&schema.WorkspaceAddonSubscription{}).
		Where("id = ?", sub.ID).
		Select("*").
		Updates(subscriptionToSchema(sub))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_addon.ErrAddonSubscriptionNotFound
	}
	return nil
}

func (r *addonSubscriptionRepository) GetByID(id string) (*workspace_addon.AddonSubscription, error) {
	var row schema.WorkspaceAddonSubscription
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_addon.ErrAddonSubscriptionNotFound
		}
		return nil, err
	}
	return subscriptionFromSchema(&row), nil
}

func (r *addonSubscriptionRepository) GetActiveByWorkspaceAndDefinition(workspaceID, addonDefinitionID string) (*workspace_addon.AddonSubscription, error) {
	var row schema.WorkspaceAddonSubscription
	err := r.db.Where("workspace_id = ? AND addon_definition_id = ? AND status = ?", workspaceID, addonDefinitionID, string(workspace_plan.SubscriptionStatusActive)).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_addon.ErrAddonSubscriptionNotFound
		}
		return nil, err
	}
	return subscriptionFromSchema(&row), nil
}

func (r *addonSubscriptionRepository) GetActiveByBoundResource(resourceType, resourceID string) (*workspace_addon.AddonSubscription, error) {
	var row schema.WorkspaceAddonSubscription
	err := r.db.Where("bound_resource_type = ? AND bound_resource_id = ? AND status = ?", resourceType, resourceID, string(workspace_plan.SubscriptionStatusActive)).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_addon.ErrAddonSubscriptionNotFound
		}
		return nil, err
	}
	return subscriptionFromSchema(&row), nil
}

func (r *addonSubscriptionRepository) ListActiveByWorkspaceAndKind(workspaceID string, kind workspace_addon.EntitlementKind) ([]*workspace_addon.AddonSubscription, error) {
	var rows []schema.WorkspaceAddonSubscription
	err := r.db.Where("workspace_id = ? AND entitlement_kind = ? AND status = ?", workspaceID, string(kind), string(workspace_plan.SubscriptionStatusActive)).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return subscriptionsFromSchema(rows), nil
}

// SumActiveGrantedUnitsByWorkspaceIDs returns, per workspace, the total granted
// units (Σ quantity * units_per_quantity) from active addon subscriptions of the
// given kind. One grouped query for the whole batch, so the reconcile job resolves
// entitlements for many workspaces without a per-workspace round trip.
func (r *addonSubscriptionRepository) SumActiveGrantedUnitsByWorkspaceIDs(workspaceIDs []string, kind workspace_addon.EntitlementKind) (map[string]int, error) {
	out := make(map[string]int, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkspaceID string
		Units       int
	}
	err := r.db.Model(&schema.WorkspaceAddonSubscription{}).
		Select("workspace_id, COALESCE(SUM(quantity * units_per_quantity), 0) AS units").
		Where("workspace_id IN ? AND entitlement_kind = ? AND status = ?",
			workspaceIDs, string(kind), string(workspace_plan.SubscriptionStatusActive)).
		Group("workspace_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.WorkspaceID] = row.Units
	}
	return out, nil
}

func (r *addonSubscriptionRepository) ListUpcomingRenewals(from, to time.Time, batchSize int) ([]*workspace_addon.AddonSubscription, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var rows []schema.WorkspaceAddonSubscription
	err := r.db.Where("status = ? AND cancelled_at IS NULL AND current_period_end > ? AND current_period_end <= ?",
		string(workspace_plan.SubscriptionStatusActive), from, to).
		Limit(batchSize).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return subscriptionsFromSchema(rows), nil
}

func (r *addonSubscriptionRepository) ListActiveByWorkspace(workspaceID string) ([]*workspace_addon.AddonSubscription, error) {
	var rows []schema.WorkspaceAddonSubscription
	err := r.db.Where("workspace_id = ? AND status = ?", workspaceID, string(workspace_plan.SubscriptionStatusActive)).
		Order("created_at asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return subscriptionsFromSchema(rows), nil
}

// ListReactivatableByWorkspace returns active addons plus the ones the cancel sweep expired this cycle.
// A sweep-expired addon is status expired, never customer-cancelled (cancelled_at IS NULL), and its
// period ended on or after expiredSince (the prior anchor) so a long-lapsed addon from an earlier cycle,
// which is not on this invoice, is not revived. The Extend the caller applies flips it back to active.
func (r *addonSubscriptionRepository) ListReactivatableByWorkspace(workspaceID string, expiredSince time.Time) ([]*workspace_addon.AddonSubscription, error) {
	var rows []schema.WorkspaceAddonSubscription
	err := r.db.Where(
		"workspace_id = ? AND (status = ? OR (status = ? AND cancelled_at IS NULL AND current_period_end >= ?))",
		workspaceID,
		string(workspace_plan.SubscriptionStatusActive),
		string(workspace_plan.SubscriptionStatusExpired),
		expiredSince,
	).Order("created_at asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return subscriptionsFromSchema(rows), nil
}

func (r *addonSubscriptionRepository) ListDueForRenewal(at time.Time, batchSize int) ([]*workspace_addon.AddonSubscription, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var rows []schema.WorkspaceAddonSubscription
	err := r.db.Where("status = ? AND current_period_end <= ?", string(workspace_plan.SubscriptionStatusActive), at).
		Order("current_period_end asc").Limit(batchSize).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return subscriptionsFromSchema(rows), nil
}

func subscriptionsFromSchema(rows []schema.WorkspaceAddonSubscription) []*workspace_addon.AddonSubscription {
	out := make([]*workspace_addon.AddonSubscription, 0, len(rows))
	for i := range rows {
		out = append(out, subscriptionFromSchema(&rows[i]))
	}
	return out
}

func subscriptionToSchema(sub *workspace_addon.AddonSubscription) *schema.WorkspaceAddonSubscription {
	return &schema.WorkspaceAddonSubscription{
		ID:                 sub.ID,
		WorkspaceID:        sub.WorkspaceID,
		AddonDefinitionID:  sub.AddonDefinitionID,
		AddonKey:           sub.AddonKey,
		EntitlementKind:    string(sub.EntitlementKind),
		Quantity:           sub.Quantity,
		UnitsPerQuantity:   sub.UnitsPerQuantity,
		BillingCycle:       string(sub.BillingCycle),
		Status:             string(sub.Status),
		UnitPriceMicros:    sub.UnitPriceMicros,
		BoundResourceType:  sub.BoundResourceType,
		BoundResourceID:    sub.BoundResourceID,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		CancelledAt:        sub.CancelledAt,
		CreatedAt:          sub.CreatedAt,
		UpdatedAt:          sub.UpdatedAt,
	}
}

func subscriptionFromSchema(row *schema.WorkspaceAddonSubscription) *workspace_addon.AddonSubscription {
	return &workspace_addon.AddonSubscription{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		AddonDefinitionID:  row.AddonDefinitionID,
		AddonKey:           row.AddonKey,
		EntitlementKind:    workspace_addon.EntitlementKind(row.EntitlementKind),
		Quantity:           row.Quantity,
		UnitsPerQuantity:   row.UnitsPerQuantity,
		BillingCycle:       workspace_plan.BillingCycle(row.BillingCycle),
		Status:             workspace_plan.SubscriptionStatus(row.Status),
		UnitPriceMicros:    row.UnitPriceMicros,
		BoundResourceType:  row.BoundResourceType,
		BoundResourceID:    row.BoundResourceID,
		CurrentPeriodStart: row.CurrentPeriodStart,
		CurrentPeriodEnd:   row.CurrentPeriodEnd,
		CancelledAt:        row.CancelledAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}
