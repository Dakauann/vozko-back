package workspace_pricing_repository

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
)

type WorkspacePricingRepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) workspace_pricing.Repository {
	return &WorkspacePricingRepositoryImpl{db: db}
}

func (r *WorkspacePricingRepositoryImpl) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	var rows []schema.PricingItem
	if err := r.db.Where("workspace_id IS NULL").Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapPricingItems(rows), nil
}

func (r *WorkspacePricingRepositoryImpl) GetPricingItem(id string) (*workspace_pricing.PricingItem, error) {
	var row schema.PricingItem
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_pricing.ErrPricingItemNotFound
		}
		return nil, err
	}
	return mapPricingItem(row), nil
}

func (r *WorkspacePricingRepositoryImpl) UpsertPricingItem(item *workspace_pricing.PricingItem) error {
	row := schema.PricingItem{
		ID:          item.ID,
		WorkspaceID: item.WorkspaceID,
		Category:    string(item.Category),
		Service:     item.Service,
		Metric:      item.Metric,
		CostMicros:  item.CostMicros,
		PriceMicros: item.PriceMicros,
		MarkupPct:   item.MarkupPct,
		Currency:    item.Currency,
	}
	return r.db.Save(&row).Error
}

func (r *WorkspacePricingRepositoryImpl) DeletePricingItem(id string) error {
	return r.db.Delete(&schema.PricingItem{}, "id = ?", id).Error
}

func (r *WorkspacePricingRepositoryImpl) SeedDefaults(items []workspace_pricing.PricingItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {

		validKeys := make(map[string]bool, len(items))
		for _, item := range items {
			key := string(item.Category) + "|" + item.Service + "|" + item.Metric
			validKeys[key] = true
		}

		var existing []schema.PricingItem
		if err := tx.Where("workspace_id IS NULL").Find(&existing).Error; err != nil {
			return err
		}
		existingByKey := make(map[string]schema.PricingItem, len(existing))
		for _, row := range existing {
			key := row.Category + "|" + row.Service + "|" + row.Metric
			existingByKey[key] = row
			if !validKeys[key] {
				if err := tx.Delete(&row).Error; err != nil {
					return err
				}
			}
		}

		for _, item := range items {
			key := string(item.Category) + "|" + item.Service + "|" + item.Metric
			if _, ok := existingByKey[key]; ok {

				continue
			}
			row := schema.PricingItem{
				ID:          uuid.New().String(),
				Category:    string(item.Category),
				Service:     item.Service,
				Metric:      item.Metric,
				CostMicros:  item.CostMicros,
				PriceMicros: item.PriceMicros,
				MarkupPct:   item.MarkupPct,
				Currency:    item.Currency,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *WorkspacePricingRepositoryImpl) CreateAuditEntry(entry *workspace_pricing.PricingAuditEntry) error {
	row := schema.PricingAuditLog{
		ID:             entry.ID,
		WorkspaceID:    entry.WorkspaceID,
		Category:       string(entry.Category),
		Service:        entry.Service,
		Metric:         entry.Metric,
		OldPriceMicros: entry.OldPriceMicros,
		NewPriceMicros: entry.NewPriceMicros,
		Currency:       entry.Currency,
		ChangedBy:      entry.ChangedBy,
	}
	return r.db.Create(&row).Error
}

func (r *WorkspacePricingRepositoryImpl) ListAuditEntries(workspaceID *string, limit, offset int) ([]workspace_pricing.PricingAuditEntry, error) {
	q := r.db.Model(&schema.PricingAuditLog{}).Order("changed_at DESC")
	if workspaceID != nil {
		q = q.Where("workspace_id = ?", *workspaceID)
	}
	if limit <= 0 {
		limit = 50
	}
	q = q.Limit(limit).Offset(offset)

	var rows []schema.PricingAuditLog
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}

	entries := make([]workspace_pricing.PricingAuditEntry, len(rows))
	for i, row := range rows {
		entries[i] = workspace_pricing.PricingAuditEntry{
			ID:             row.ID,
			WorkspaceID:    row.WorkspaceID,
			Category:       workspace_pricing.ServiceCategory(row.Category),
			Service:        row.Service,
			Metric:         row.Metric,
			OldPriceMicros: row.OldPriceMicros,
			NewPriceMicros: row.NewPriceMicros,
			Currency:       row.Currency,
			ChangedBy:      row.ChangedBy,
			ChangedAt:      row.ChangedAt,
		}
	}
	return entries, nil
}

func mapPricingItems(rows []schema.PricingItem) []workspace_pricing.PricingItem {
	items := make([]workspace_pricing.PricingItem, len(rows))
	for i, row := range rows {
		items[i] = *mapPricingItem(row)
	}
	return items
}

func mapPricingItem(row schema.PricingItem) *workspace_pricing.PricingItem {
	return &workspace_pricing.PricingItem{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Category:    workspace_pricing.ServiceCategory(row.Category),
		Service:     row.Service,
		Metric:      row.Metric,
		CostMicros:  row.CostMicros,
		PriceMicros: row.PriceMicros,
		MarkupPct:   row.MarkupPct,
		Currency:    row.Currency,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
