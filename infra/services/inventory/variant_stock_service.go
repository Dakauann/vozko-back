package inventory_service

import (
	"errors"

	"vozko/domain/inventory"
	"vozko/domain/order"
	"vozko/domain/product"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type variantStockService struct {
	db *gorm.DB
}

func NewVariantStockService(db *gorm.DB) inventory.VariantStockService {
	return &variantStockService{db: db}
}

func (s *variantStockService) GetSnapshots(variantIDs []string) (map[string]inventory.VariantStockSnapshot, error) {
	snapshots := make(map[string]inventory.VariantStockSnapshot, len(variantIDs))
	if len(variantIDs) == 0 {
		return snapshots, nil
	}

	var baseRows []struct {
		ID        string
		Inventory int
	}
	if err := s.db.Table("variants").
		Select("id, inventory").
		Where("id IN ?", variantIDs).
		Scan(&baseRows).Error; err != nil {
		return nil, err
	}

	for _, row := range baseRows {
		snapshots[row.ID] = inventory.VariantStockSnapshot{
			BaseInventory: row.Inventory,
		}
	}

	if err := s.populateLaunched(variantIDs, snapshots); err != nil {
		return nil, err
	}
	if err := s.populateReserved(variantIDs, snapshots); err != nil {
		return nil, err
	}
	if err := s.populateSold(variantIDs, snapshots); err != nil {
		return nil, err
	}

	return snapshots, nil
}

func (s *variantStockService) GetSnapshot(variantID string) (inventory.VariantStockSnapshot, error) {
	snapshots, err := s.GetSnapshots([]string{variantID})
	if err != nil {
		return inventory.VariantStockSnapshot{}, err
	}
	if snap, ok := snapshots[variantID]; ok {
		return snap, nil
	}
	return inventory.VariantStockSnapshot{}, product.ErrVariantNotFound
}

func (s *variantStockService) RecordLaunch(variantID string, quantity int, metadata inventory.VariantStockMetadata) (inventory.VariantStockSnapshot, error) {
	if quantity <= 0 {
		return inventory.VariantStockSnapshot{}, product.ErrVariantStockAdjustmentInvalid
	}

	var snapshot inventory.VariantStockSnapshot
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var variant schema.Variant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", variantID).
			First(&variant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return product.ErrVariantNotFound
			}
			return err
		}

		if metadata.ProductID != "" && variant.ProductID != metadata.ProductID {
			return product.ErrVariantNotFound
		}

		adjustment := schema.VariantStockAdjustment{
			VariantID: variantID,
			Quantity:  quantity,
			Note:      metadata.Note,
			CreatedBy: metadata.ActorID,
		}

		if err := tx.Create(&adjustment).Error; err != nil {
			return err
		}

		tmpService := &variantStockService{db: tx}
		snapshots, err := tmpService.GetSnapshots([]string{variantID})
		if err != nil {
			return err
		}

		snap := snapshots[variantID]
		snap.BaseInventory = variant.Inventory
		snapshot = snap
		return nil
	})

	return snapshot, err
}

func (s *variantStockService) populateLaunched(variantIDs []string, snapshots map[string]inventory.VariantStockSnapshot) error {
	var rows []struct {
		VariantID string
		Total     int
	}

	if err := s.db.Table("variant_stock_adjustments").
		Select("variant_id, COALESCE(SUM(quantity), 0) as total").
		Where("variant_id IN ?", variantIDs).
		Group("variant_id").
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		entry := snapshots[row.VariantID]
		entry.Launched = row.Total
		snapshots[row.VariantID] = entry
	}

	return nil
}

func (s *variantStockService) populateReserved(variantIDs []string, snapshots map[string]inventory.VariantStockSnapshot) error {
	if len(variantIDs) == 0 {
		return nil
	}

	statuses := []string{string(order.StatusPending), string(order.StatusProcessing)}
	var rows []struct {
		VariantID string
		Total     int
	}

	if err := s.db.Table("order_items AS oi").
		Select("oi.variant_id, COALESCE(SUM(oi.quantity), 0) as total").
		Joins("JOIN orders o ON o.id = oi.order_id").
		Where("oi.variant_id IN ? AND o.status IN ?", variantIDs, statuses).
		Group("oi.variant_id").
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		entry := snapshots[row.VariantID]
		entry.Reserved = row.Total
		snapshots[row.VariantID] = entry
	}

	return nil
}

func (s *variantStockService) populateSold(variantIDs []string, snapshots map[string]inventory.VariantStockSnapshot) error {
	if len(variantIDs) == 0 {
		return nil
	}

	statuses := []string{
		string(order.StatusPaid),
		string(order.StatusShipped),
		string(order.StatusDelivered),
	}

	var rows []struct {
		VariantID string
		Total     int
	}

	if err := s.db.Table("order_items AS oi").
		Select("oi.variant_id, COALESCE(SUM(oi.quantity), 0) as total").
		Joins("JOIN orders o ON o.id = oi.order_id").
		Where("oi.variant_id IN ? AND o.status IN ?", variantIDs, statuses).
		Group("oi.variant_id").
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		entry := snapshots[row.VariantID]
		entry.Sold = row.Total
		snapshots[row.VariantID] = entry
	}

	return nil
}
