package workspace_pricing

type Repository interface {
	ListDefaultPricingItems() ([]PricingItem, error)
	GetPricingItem(id string) (*PricingItem, error)
	UpsertPricingItem(item *PricingItem) error
	DeletePricingItem(id string) error
	SeedDefaults(items []PricingItem) error

	CreateAuditEntry(entry *PricingAuditEntry) error
	ListAuditEntries(workspaceID *string, limit, offset int) ([]PricingAuditEntry, error)
}
