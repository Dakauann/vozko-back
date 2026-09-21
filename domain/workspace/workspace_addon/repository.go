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
	ListReactivatableByWorkspace(workspaceID string, expiredSince time.Time) ([]*AddonSubscription, error)
	ListDueForRenewal(at time.Time, batchSize int) ([]*AddonSubscription, error)
	ListUpcomingRenewals(from, to time.Time, batchSize int) ([]*AddonSubscription, error)
	SumActiveGrantedUnitsByWorkspaceIDs(workspaceIDs []string, kind EntitlementKind) (map[string]int, error)
}

type AddonDefinitionReader interface {
	GetByID(id string) (*AddonDefinition, error)
}

type AddonSubscriptionReader interface {
	ListActiveByWorkspaceAndKind(workspaceID string, kind EntitlementKind) ([]*AddonSubscription, error)
}
