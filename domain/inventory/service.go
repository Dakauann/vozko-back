package inventory

type VariantStockSnapshot struct {
	BaseInventory int
	Launched      int
	Reserved      int
	Sold          int
}

func (s VariantStockSnapshot) Available() int {
	available := s.BaseInventory + s.Launched - s.Reserved - s.Sold
	if available < 0 {
		return 0
	}
	return available
}

type VariantStockService interface {
	GetSnapshots(variantIDs []string) (map[string]VariantStockSnapshot, error)
	GetSnapshot(variantID string) (VariantStockSnapshot, error)
	RecordLaunch(variantID string, quantity int, metadata VariantStockMetadata) (VariantStockSnapshot, error)
}

type VariantStockMetadata struct {
	ActorID   string
	ProductID string
	Note      string
}
