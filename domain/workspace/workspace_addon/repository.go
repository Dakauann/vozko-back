package workspace_addon

import "time"

type AddonDefinitionRepository interface {
	Create(def *AddonDefinition) error
	Update(def *AddonDefinition) error
	Archive(id string, archivedAt time.Time) error
	GetByID(id string) (*AddonDefinition, error)
	GetByKey(key string) (*AddonDefinition, error)
	List(includeArchived bool) ([]*AddonDefinition, error)
	ListActiveVisible(workspaceID string) ([]*AddonDefinition, error)
}

type AddonSubscriptionRepository interface {
	Create(sub *AddonSubscription) error
	Update(sub *AddonSubscription) error
	GetByID(id string) (*AddonSubscription, error)
	GetActiveByWorkspaceAndDefinition(workspaceID, addonDefinitionID string) (*AddonSubscription, error)
	GetActiveByBoundResource(resourceType, resourceID string) (*AddonSubscription, error)
	ListActiveByWorkspaceAndKind(workspaceID string, kind EntitlementKind) ([]*AddonSubscription, error)
	ListActiveByWorkspace(workspaceID string) ([]*AddonSubscription, error)
	// ListReactivatableByWorkspace returns the addons a paid unified monthly invoice should extend or
	// revive: every active addon, plus addons the cancel sweep expired this cycle (status expired,
	// never customer-cancelled, period end on or after expiredSince). A late payment after the sweep
	// thus restores the very channels the sweep suspended, while expiredSince (the prior anchor)
	// excludes addons that lapsed in an earlier cycle and are not part of this invoice.
	ListReactivatableByWorkspace(workspaceID string, expiredSince time.Time) ([]*AddonSubscription, error)
	ListDueForRenewal(at time.Time, batchSize int) ([]*AddonSubscription, error)
	// ListUpcomingRenewals returns active, non-cancelled addons whose period ends in
	// the (from, to] window, for the "renews soon" reminder.
	ListUpcomingRenewals(from, to time.Time, batchSize int) ([]*AddonSubscription, error)
	// SumActiveGrantedUnitsByWorkspaceIDs returns, per workspace, the total granted
	// units from active addons of the kind, in one grouped query (batch resolution).
	SumActiveGrantedUnitsByWorkspaceIDs(workspaceIDs []string, kind EntitlementKind) (map[string]int, error)
}

type AddonDefinitionReader interface {
	GetByID(id string) (*AddonDefinition, error)
}

type AddonSubscriptionReader interface {
	ListActiveByWorkspaceAndKind(workspaceID string, kind EntitlementKind) ([]*AddonSubscription, error)
}
